-- name: CreatePerson :one
insert into people (
  id,
  household_id,
  name,
  user_id,
  max_portion_grams,
  min_protein_grams,
  max_carbohydrate_grams,
  min_pairing_ratio,
  max_sugars_grams,
  warn_fast_carbs,
  notes,
  profile
) values (
  @id,
  @household_id,
  @name,
  @user_id,
  @max_portion_grams,
  @min_protein_grams,
  @max_carbohydrate_grams,
  @min_pairing_ratio,
  @max_sugars_grams,
  @warn_fast_carbs,
  @notes,
  @profile
)
returning *;

-- name: GetPerson :one
select * from people where id = @id and household_id = @household_id;

-- name: ListPeople :many
select * from people
where household_id = @household_id
order by lower(name), id;

-- name: UpdatePerson :one
update people set
  name = @name,
  user_id = @user_id,
  max_portion_grams = @max_portion_grams,
  min_protein_grams = @min_protein_grams,
  max_carbohydrate_grams = @max_carbohydrate_grams,
  min_pairing_ratio = @min_pairing_ratio,
  max_sugars_grams = @max_sugars_grams,
  warn_fast_carbs = @warn_fast_carbs,
  notes = @notes,
  profile = @profile,
  update_time = now()
where id = @id and household_id = @household_id
returning *;

-- name: DeletePerson :execrows
delete from people where id = @id and household_id = @household_id;

-- name: DeleteOtherEatingTimes :exec
-- Deletes the person's eating times other than the given ones, with their
-- portions and staples.
delete from eating_times
where person_id = @person_id
  and household_id = @household_id
  and not (id = any(@keep_ids::uuid[]));

-- name: UpsertEatingTimes :exec
-- Creates or updates a person's eating times, given as arrays of their
-- fields. An ID of another person's eating time is left alone, rather than
-- moved to this person.
insert into eating_times (
  id,
  household_id,
  person_id,
  position,
  name,
  kind,
  time_of_day,
  meal
)
select
  (@ids::uuid[])[i],
  @household_id,
  @person_id,
  i - 1,
  (@names::text[])[i],
  ((@kinds::text[])[i])::eating_time_kind,
  (@times::text[])[i],
  nullif((@meals::text[])[i], '')::household_meal
from generate_subscripts(@ids::uuid[], 1) as i
on conflict (id) do update set
  position = excluded.position,
  name = excluded.name,
  kind = excluded.kind,
  time_of_day = excluded.time_of_day,
  meal = excluded.meal
where eating_times.person_id = excluded.person_id;

-- name: ListEatingTimes :many
-- The eating times of the given people, or of all the household's people,
-- in order.
select * from eating_times
where household_id = @household_id
  and (@all_people::boolean or person_id = any(@person_ids::uuid[]))
order by person_id, position, id;

-- name: DeleteTargets :exec
delete from person_targets
where person_id = @person_id and household_id = @household_id;

-- name: CreateTargets :exec
-- The targets are given as arrays of their fields, with NaN for an unset
-- bound, as arrays of nullable numbers can't be sent.
insert into person_targets (household_id, person_id, nutrient, min_per_day, max_per_day)
select
  @household_id,
  @person_id,
  ((@nutrients::text[])[i])::nutrient,
  nullif((@mins::double precision[])[i], 'NaN'),
  nullif((@maxes::double precision[])[i], 'NaN')
from generate_subscripts(@nutrients::text[], 1) as i;

-- name: ListTargets :many
select * from person_targets
where household_id = @household_id
  and (@all_people::boolean or person_id = any(@person_ids::uuid[]))
order by person_id, nutrient;
