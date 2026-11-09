-- name: CreateCook :execrows
insert into cooks (id, household_id, recipe_id, date, servings, meal, notes, done, frozen_servings, off_plan_servings)
values (@id, @household_id, @recipe_id, @date, @servings, @meal, @notes, @done, @frozen_servings, @off_plan_servings);

-- name: GrowCook :execrows
-- Adds servings to a cook, for portions of it planned later, like the rest
-- of a dinner eaten as a snack.
update cooks set servings = servings + @servings, update_time = now()
where id = @id and household_id = @household_id;

-- name: UpdateCook :execrows
update cooks set
  recipe_id = @recipe_id,
  date = @date,
  servings = @servings,
  meal = @meal,
  notes = @notes,
  done = @done,
  frozen_servings = @frozen_servings,
  off_plan_servings = @off_plan_servings,
  update_time = now()
where id = @id and household_id = @household_id;

-- name: DeleteOrphanSuppliers :exec
-- Deletes the cooks that only supply a cook, and are neither eaten nor
-- cooked yet, as they were planned for it: e.g. the chickpeas of a chili
-- deleted.
delete from cooks
where cooks.household_id = @household_id
  and cooks.id in (select cs.supplier_cook_id from cook_supplies cs where cs.consumer_cook_id = @consumer_cook_id)
  and not cooks.done
  and not exists (select 1 from portions p where p.cook_id = cooks.id)
  and not exists (
    select 1 from cook_supplies s
    where s.supplier_cook_id = cooks.id and s.consumer_cook_id <> @consumer_cook_id
  );

-- name: DeleteCook :execrows
delete from cooks where id = @id and household_id = @household_id;

-- name: CreateCookSupplies :exec
-- Links cooks to the cooks supplying their components, given as arrays.
insert into cook_supplies (household_id, consumer_cook_id, component_recipe_id, supplier_cook_id)
select
  @household_id,
  (@consumer_cook_ids::uuid[])[i],
  (@component_recipe_ids::uuid[])[i],
  (@supplier_cook_ids::uuid[])[i]
from generate_subscripts(@consumer_cook_ids::uuid[], 1) as i;

-- name: SetCookSupply :execrows
-- Sets the cook supplying a component of a cook.
insert into cook_supplies (household_id, consumer_cook_id, component_recipe_id, supplier_cook_id)
values (@household_id, @consumer_cook_id, @component_recipe_id, @supplier_cook_id)
on conflict (consumer_cook_id, component_recipe_id) do update set supplier_cook_id = excluded.supplier_cook_id;

-- name: ListCookSupplies :many
-- The links between the household's cooks of which either is given by ID
-- or cooked in a range of dates, with both cooks' dates and recipes, and
-- the servings the supplier makes for the consumer.
select
  s.consumer_cook_id,
  s.component_recipe_id,
  s.supplier_cook_id,
  cc.date as consumer_date,
  cr.name as consumer_recipe_name,
  sc.date as supplier_date,
  sr.name as supplier_recipe_name,
  (coalesce((
    select sum(ri.amount) from recipe_ingredients ri
    where ri.recipe_id = cr.id and ri.component_recipe_id = s.component_recipe_id and not ri.optional
  ), 0) * cc.servings / cr.servings)::double precision as servings
from cook_supplies s
join cooks cc on cc.id = s.consumer_cook_id
join recipes cr on cr.id = cc.recipe_id
join cooks sc on sc.id = s.supplier_cook_id
join recipes sr on sr.id = sc.recipe_id
where s.household_id = @household_id
  and (
    s.consumer_cook_id = any(@ids::uuid[])
    or s.supplier_cook_id = any(@ids::uuid[])
    or cc.date between sqlc.narg(from_date)::date and sqlc.narg(to_date)::date
    or sc.date between sqlc.narg(from_date)::date and sqlc.narg(to_date)::date
  )
order by cc.date, s.consumer_cook_id, s.component_recipe_id;

-- name: ListCooks :many
-- The household's cooks, given by ID or cooked in a range of dates, with
-- their recipe's name, the servings of all their portions, whatever their
-- date, and of those eaten after the recipe's fridge days, which come from
-- the freezer.
select
  sqlc.embed(cooks),
  r.name as recipe_name,
  coalesce((select sum(p.servings) from portions p where p.cook_id = cooks.id), 0)::double precision as assigned_servings,
  coalesce((
    select sum(p.servings) from portions p
    where p.cook_id = cooks.id and r.fridge_days is not null and p.date > cooks.date + r.fridge_days
  ), 0)::double precision as late_servings,
  coalesce((
    select sum(ri.amount * cc.servings / cr.servings)
    from cook_supplies s
    join cooks cc on cc.id = s.consumer_cook_id
    join recipes cr on cr.id = cc.recipe_id
    join recipe_ingredients ri on ri.recipe_id = cr.id and ri.component_recipe_id = s.component_recipe_id and not ri.optional
    where s.supplier_cook_id = cooks.id
  ), 0)::double precision as supplied_servings
from cooks
join recipes r on r.id = cooks.recipe_id
where cooks.household_id = @household_id
  and (
    cooks.id = any(@ids::uuid[])
    or cooks.date between sqlc.narg(from_date)::date and sqlc.narg(to_date)::date
  )
order by cooks.date, cooks.create_time, cooks.id;

-- name: CreatePortion :execrows
insert into portions (
  id, household_id, person_id, eating_time_id, date, cook_id, servings, food_id, amount, unit, notes
) values (
  @id, @household_id, @person_id, @eating_time_id, @date, @cook_id, @servings, @food_id, @amount, @unit, @notes
);

-- name: CreateCookPortions :execrows
-- Creates portions of a cook, given as arrays of their fields.
insert into portions (id, household_id, person_id, eating_time_id, date, cook_id, servings)
select
  (@ids::uuid[])[i],
  @household_id,
  (@person_ids::uuid[])[i],
  (@eating_time_ids::uuid[])[i],
  (@dates::date[])[i],
  @cook_id,
  (@servings::double precision[])[i]
from generate_subscripts(@ids::uuid[], 1) as i;

-- name: UpdatePortion :execrows
update portions set
  person_id = @person_id,
  eating_time_id = @eating_time_id,
  date = @date,
  cook_id = @cook_id,
  servings = @servings,
  food_id = @food_id,
  amount = @amount,
  unit = @unit,
  notes = @notes,
  update_time = now()
where id = @id and household_id = @household_id;

-- name: DeletePortion :execrows
delete from portions where id = @id and household_id = @household_id;

-- name: ListPortions :many
-- The household's portions, given by ID or eaten in a range of dates, with
-- the name of their recipe or food, and their cook's date and recipe.
select
  sqlc.embed(portions),
  coalesce(r.name, f.name, '')::text as name,
  c.date as cook_date,
  c.recipe_id as cook_recipe_id,
  c.frozen_servings as cook_frozen_servings
from portions
left join cooks c on c.id = portions.cook_id
left join recipes r on r.id = c.recipe_id
left join foods f on f.id = portions.food_id
where portions.household_id = @household_id
  and (
    portions.id = any(@ids::uuid[])
    or portions.date between sqlc.narg(from_date)::date and sqlc.narg(to_date)::date
  )
order by portions.date, portions.create_time, portions.id;

-- name: CreateStaple :execrows
insert into staples (
  id, household_id, person_id, eating_time_id, recipe_id, servings, food_id, amount, unit, weekdays, start_date, end_date, notes, replaces_meal
) values (
  @id, @household_id, @person_id, @eating_time_id, @recipe_id, @servings, @food_id, @amount, @unit, @weekdays, @start_date, @end_date, @notes, @replaces_meal
);

-- name: UpdateStaple :execrows
update staples set
  person_id = @person_id,
  eating_time_id = @eating_time_id,
  recipe_id = @recipe_id,
  servings = @servings,
  food_id = @food_id,
  amount = @amount,
  unit = @unit,
  weekdays = @weekdays,
  start_date = @start_date,
  end_date = @end_date,
  notes = @notes,
  replaces_meal = @replaces_meal,
  update_time = now()
where id = @id and household_id = @household_id;

-- name: DeleteStaple :execrows
delete from staples where id = @id and household_id = @household_id;

-- name: ListStaples :many
-- The household's staples, all, or given by ID or person, or on some date
-- in a range, with the name of their recipe or food, in the order of their
-- people and eating times.
select
  sqlc.embed(staples),
  coalesce(r.name, f.name, '')::text as name
from staples
join people pe on pe.id = staples.person_id
join eating_times et on et.id = staples.eating_time_id
left join recipes r on r.id = staples.recipe_id
left join foods f on f.id = staples.food_id
where staples.household_id = @household_id
  and (sqlc.narg(id)::uuid is null or staples.id = sqlc.narg(id))
  and (sqlc.narg(person_id)::uuid is null or staples.person_id = sqlc.narg(person_id))
  and (sqlc.narg(to_date)::date is null or staples.start_date is null or staples.start_date <= sqlc.narg(to_date))
  and (sqlc.narg(from_date)::date is null or staples.end_date is null or staples.end_date >= sqlc.narg(from_date))
order by lower(pe.name), pe.id, et.position, staples.create_time, staples.id;

-- name: SkipStaple :exec
insert into staple_skips (household_id, staple_id, date)
select s.household_id, s.id, @date from staples s
where s.id = @staple_id and s.household_id = @household_id
on conflict do nothing;

-- name: UnskipStaple :exec
delete from staple_skips
where staple_id = @staple_id and household_id = @household_id and date = @date;

-- name: ListStapleSkips :many
-- The dates the household's staples, or one of them, are skipped on, in a
-- range of dates or all.
select * from staple_skips
where household_id = @household_id
  and (sqlc.narg(staple_id)::uuid is null or staple_id = sqlc.narg(staple_id))
  and (sqlc.narg(from_date)::date is null or date >= sqlc.narg(from_date))
  and (sqlc.narg(to_date)::date is null or date <= sqlc.narg(to_date))
order by staple_id, date;

-- name: ListFrozenCooks :many
-- The household's cooks with frozen servings not all planned or taken off
-- plan yet: those in the freezer, with their recipe's name and keeping
-- days, and the servings of their portions eaten after the recipe's fridge
-- days, by date.
select
  sqlc.embed(cooks),
  r.name as recipe_name,
  r.fridge_days,
  r.freezer_days,
  late.servings::double precision as late_servings
from cooks
join recipes r on r.id = cooks.recipe_id
cross join lateral (
  select coalesce(sum(p.servings), 0) as servings from portions p
  where p.cook_id = cooks.id and r.fridge_days is not null and p.date > cooks.date + r.fridge_days
) late
where cooks.household_id = @household_id and cooks.frozen_servings > late.servings + cooks.off_plan_servings + 0.01
order by cooks.date, cooks.id;

-- name: DeleteUnusedCook :execrows
-- Deletes a cook nobody eats, that supplies no other cook and isn't cooked
-- yet: a supplier replaced by another.
delete from cooks
where cooks.household_id = @household_id
  and cooks.id = @id
  and not cooks.done
  and not exists (select 1 from portions p where p.cook_id = cooks.id)
  and not exists (select 1 from cook_supplies s where s.supplier_cook_id = cooks.id);
