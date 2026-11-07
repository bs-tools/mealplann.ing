-- name: CreateCook :execrows
insert into cooks (id, household_id, recipe_id, date, servings, meal, notes, done)
values (@id, @household_id, @recipe_id, @date, @servings, @meal, @notes, @done);

-- name: UpdateCook :execrows
update cooks set
  recipe_id = @recipe_id,
  date = @date,
  servings = @servings,
  meal = @meal,
  notes = @notes,
  done = @done,
  update_time = now()
where id = @id and household_id = @household_id;

-- name: DeleteCook :execrows
delete from cooks where id = @id and household_id = @household_id;

-- name: ListCooks :many
-- The household's cooks, given by ID or cooked in a range of dates, with
-- their recipe's name, and the servings of all their portions, whatever
-- their date.
select
  sqlc.embed(cooks),
  r.name as recipe_name,
  coalesce((select sum(p.servings) from portions p where p.cook_id = cooks.id), 0)::double precision as assigned_servings
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
  c.recipe_id as cook_recipe_id
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
  id, household_id, person_id, eating_time_id, recipe_id, servings, food_id, amount, unit, weekdays, start_date, end_date, notes
) values (
  @id, @household_id, @person_id, @eating_time_id, @recipe_id, @servings, @food_id, @amount, @unit, @weekdays, @start_date, @end_date, @notes
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
