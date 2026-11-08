-- name: ListPantryItems :many
-- The household's lots, all, or given by ID or food, with what the pantry
-- needs of their food, ordered by food name and expiry date.
select
  sqlc.embed(pantry_items),
  f.name as food_name,
  f.store_section,
  f.grams_per_piece,
  f.opened_shelf_life_days,
  f.freezer_shelf_life_days
from pantry_items
join foods f on f.id = pantry_items.food_id
where pantry_items.household_id = @household_id
  and (sqlc.narg(ids)::uuid[] is null or pantry_items.id = any(sqlc.narg(ids)::uuid[]))
  and (sqlc.narg(food_ids)::uuid[] is null or pantry_items.food_id = any(sqlc.narg(food_ids)::uuid[]))
order by lower(f.name), pantry_items.expiry_date nulls last, pantry_items.create_time, pantry_items.id;

-- name: LockPantryItems :many
-- The household's lots of some foods, locked until the end of the
-- transaction, for a cook to take its ingredients out of.
select
  sqlc.embed(pantry_items),
  f.name as food_name,
  f.store_section,
  f.grams_per_piece,
  f.opened_shelf_life_days,
  f.freezer_shelf_life_days
from pantry_items
join foods f on f.id = pantry_items.food_id
where pantry_items.household_id = @household_id
  and pantry_items.food_id = any(@food_ids::uuid[])
order by pantry_items.id
for update of pantry_items;

-- name: CreatePantryItems :execrows
-- Creates lots, given as arrays of their fields.
insert into pantry_items (
  id, household_id, food_id, grams, expiry_date, opened_date, notes,
  storage, location, frozen_date, opened, estimated, label
)
select
  (@ids::uuid[])[i],
  @household_id,
  (@food_ids::uuid[])[i],
  (@grams::double precision[])[i],
  (@expiry_dates::date[])[i],
  (@opened_dates::date[])[i],
  (@notes::text[])[i],
  -- Storages are text, as an array of a nullable enum, with null for
  -- unspecified, doesn't pass as a parameter.
  nullif((@storages::text[])[i], '')::pantry_storage,
  (@locations::text[])[i],
  (@frozen_dates::date[])[i],
  (@opened::boolean[])[i],
  (@estimated::boolean[])[i],
  (@labels::text[])[i]
from generate_subscripts(@ids::uuid[], 1) as i;

-- name: UpdatePantryItem :execrows
update pantry_items set
  grams = @grams,
  expiry_date = @expiry_date,
  opened_date = @opened_date,
  notes = @notes,
  storage = sqlc.narg(storage),
  location = @location,
  frozen_date = @frozen_date,
  opened = @opened,
  estimated = @estimated,
  label = @label,
  update_time = now()
where id = @id and household_id = @household_id;

-- name: DeletePantryItem :execrows
delete from pantry_items where id = @id and household_id = @household_id;

-- name: SetPantryItemGrams :exec
-- Sets the grams of lots, given as arrays.
update pantry_items p set
  grams = (@grams::double precision[])[v.i],
  update_time = now()
from generate_subscripts(@ids::uuid[], 1) as v(i)
where p.id = (@ids::uuid[])[v.i] and p.household_id = @household_id;

-- name: DeletePantryItems :exec
delete from pantry_items where household_id = @household_id and id = any(@ids::uuid[]);

-- name: CreateCookPantryUse :execrows
-- Records that a cook took its ingredients out of the pantry, unless it
-- already did, which affects no rows.
insert into cook_pantry_uses (cook_id, household_id)
values (@cook_id, @household_id)
on conflict do nothing;

-- name: CreateCookPantryDeductions :exec
-- Records what a cook took out of each lot, given as arrays.
insert into cook_pantry_deductions (
  cook_id, household_id, pantry_item_id, food_id, grams, expiry_date, opened_date, notes,
  storage, location, frozen_date, opened, estimated, label
)
select
  @cook_id,
  @household_id,
  (@pantry_item_ids::uuid[])[i],
  (@food_ids::uuid[])[i],
  (@grams::double precision[])[i],
  (@expiry_dates::date[])[i],
  (@opened_dates::date[])[i],
  (@notes::text[])[i],
  nullif((@storages::text[])[i], '')::pantry_storage,
  (@locations::text[])[i],
  (@frozen_dates::date[])[i],
  (@opened::boolean[])[i],
  (@estimated::boolean[])[i],
  (@labels::text[])[i]
from generate_subscripts(@pantry_item_ids::uuid[], 1) as i;

-- name: RestoreCookPantryUse :many
-- Puts back what a cook took out of the pantry, into the same lots,
-- recreating those it emptied, and forgets that it did, all in one
-- statement. Deleting the use deletes its deductions at the end of the
-- statement, after they're read. Returns the deductions put back, none if
-- the cook took nothing out.
with used as (
  delete from cook_pantry_uses u
  where u.cook_id = @cook_id and u.household_id = @household_id
  returning u.cook_id
), restored as (
  select d.* from cook_pantry_deductions d
  join used on used.cook_id = d.cook_id
), put_back as (
  insert into pantry_items (
    id, household_id, food_id, grams, expiry_date, opened_date, notes,
    storage, location, frozen_date, opened, estimated, label
  )
  select
    r.pantry_item_id, r.household_id, r.food_id, r.grams, r.expiry_date, r.opened_date, r.notes,
    r.storage, r.location, r.frozen_date, r.opened, r.estimated, r.label
  from restored r
  on conflict (id) do update set
    grams = pantry_items.grams + excluded.grams,
    update_time = now()
  returning pantry_items.id
)
select r.food_id, r.grams from restored r
order by r.food_id, r.pantry_item_id;
