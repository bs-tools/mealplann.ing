-- name: CreateStore :one
-- Creates a store after the household's others.
insert into stores (household_id, name, position)
values (
  @household_id,
  @name,
  coalesce((select max(s.position) + 1 from stores s where s.household_id = @household_id), 0)
)
returning *;

-- name: GetStore :one
select * from stores where id = @id and household_id = @household_id;

-- name: ListStores :many
select * from stores
where household_id = @household_id
order by position, create_time, id;

-- name: UpdateStore :one
update stores set
  name = @name,
  position = @position,
  update_time = now()
where id = @id and household_id = @household_id
returning *;

-- name: DeleteStore :execrows
delete from stores where id = @id and household_id = @household_id;

-- name: CreateShoppingList :one
insert into shopping_lists (id, household_id, start_date, end_date)
values (@id, @household_id, @start_date, @end_date)
returning *;

-- name: GetShoppingList :one
select * from shopping_lists where id = @id and household_id = @household_id;

-- name: ListShoppingLists :many
-- The household's lists, latest first, with their numbers of items.
select
  sqlc.embed(shopping_lists),
  (select count(*) from shopping_items i where i.list_id = shopping_lists.id)::integer as item_count,
  (select count(*) from shopping_items i where i.list_id = shopping_lists.id and i.checked)::integer as checked_count,
  (select count(*) from shopping_items i where i.list_id = shopping_lists.id and i.top_up)::integer as top_up_count
from shopping_lists
where shopping_lists.household_id = @household_id
order by shopping_lists.start_date desc, shopping_lists.create_time desc, shopping_lists.id
limit @row_limit offset @row_offset;

-- name: TouchShoppingList :execrows
-- Records that a list was generated again, unless it's complete.
update shopping_lists set update_time = now()
where id = @id and household_id = @household_id and complete_time is null;

-- name: CompleteShoppingList :execrows
-- Marks a list complete, unless it already is.
update shopping_lists set complete_time = now(), update_time = now()
where id = @id and household_id = @household_id and complete_time is null;

-- name: ReopenShoppingList :exec
update shopping_lists set complete_time = null, update_time = now()
where id = @id and household_id = @household_id;

-- name: DeleteShoppingList :execrows
delete from shopping_lists where id = @id and household_id = @household_id;

-- name: ListShoppingItems :many
-- The items of a household's list, or given by ID, with the name of who
-- checked them, their person's name in the household or their passkey's,
-- and their store, or the main store if they have none.
select
  sqlc.embed(shopping_items),
  coalesce(pe.name, pu.name, '')::text as checked_by_name,
  st.id as resolved_store_id,
  coalesce(st.name, '')::text as store_name
from shopping_items
left join people pe on pe.household_id = shopping_items.household_id and pe.user_id = shopping_items.checked_by
left join passkey_users pu on pu.id = shopping_items.checked_by
left join lateral (
  select s.id, s.name from stores s
  where s.household_id = shopping_items.household_id
  order by s.id is not distinct from shopping_items.store_id desc, s.position, s.create_time, s.id
  limit 1
) st on true
where shopping_items.household_id = @household_id
  and (sqlc.narg(list_id)::uuid is null or shopping_items.list_id = sqlc.narg(list_id))
  and (sqlc.narg(id)::uuid is null or shopping_items.id = sqlc.narg(id))
order by shopping_items.create_time, shopping_items.id;

-- name: CreateShoppingItems :execrows
-- Creates items of a list, given as arrays of their fields. Store sections
-- are text, with '' for none, as arrays of enums can't hold nulls.
insert into shopping_items (
  id, household_id, list_id, food_id, name, grams, needed_grams, pantry_grams, packages,
  amount_text, store_id, store_section, needed_by, top_up, warning, checked, checked_by,
  check_time, note, manual
)
select
  (@ids::uuid[])[i],
  @household_id,
  @list_id,
  (@food_ids::uuid[])[i],
  (@names::text[])[i],
  (@grams::double precision[])[i],
  (@needed_grams::double precision[])[i],
  (@pantry_grams::double precision[])[i],
  (@packages::integer[])[i],
  (@amount_texts::text[])[i],
  (@store_ids::uuid[])[i],
  nullif((@store_sections::text[])[i], '')::store_section,
  (@needed_by::date[])[i],
  (@top_ups::boolean[])[i],
  (@warnings::text[])[i],
  (@checked::boolean[])[i],
  (@checked_by::uuid[])[i],
  (@check_times::timestamptz[])[i],
  (@notes::text[])[i],
  (@manual::boolean[])[i]
from generate_subscripts(@ids::uuid[], 1) as i;

-- name: DeleteGeneratedShoppingItems :exec
delete from shopping_items
where list_id = @list_id and household_id = @household_id and not manual;

-- name: SetShoppingItemChecked :one
-- Checks or unchecks an item, and returns it with the name of who checked
-- it, and its store, or the main store if it has none, in one round trip.
-- Checking a checked item keeps who checked it.
with updated as (
  update shopping_items i set
    checked = @checked::boolean,
    checked_by = case when not @checked::boolean then null when i.checked then i.checked_by else @user_id::uuid end,
    check_time = case when not @checked::boolean then null when i.checked then i.check_time else now() end,
    update_time = now()
  where i.id = @id and i.household_id = @household_id
  returning i.*
)
select
  updated.id, updated.household_id, updated.list_id, updated.food_id, updated.name,
  updated.grams, updated.needed_grams, updated.pantry_grams, updated.packages,
  updated.amount_text, updated.store_id, updated.store_section, updated.needed_by,
  updated.top_up, updated.warning, updated.checked, updated.checked_by,
  updated.check_time, updated.note, updated.manual, updated.create_time, updated.update_time,
  coalesce(pe.name, pu.name, '')::text as checked_by_name,
  st.id as resolved_store_id,
  coalesce(st.name, '')::text as store_name
from updated
left join people pe on pe.household_id = updated.household_id and pe.user_id = updated.checked_by
left join passkey_users pu on pu.id = updated.checked_by
left join lateral (
  select s.id, s.name from stores s
  where s.household_id = updated.household_id
  order by s.id is not distinct from updated.store_id desc, s.position, s.create_time, s.id
  limit 1
) st on true;

-- name: DeleteShoppingItem :execrows
delete from shopping_items where id = @id and household_id = @household_id;
