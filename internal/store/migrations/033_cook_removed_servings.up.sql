-- How many of a cook's servings were taken out of the fridge without a
-- portion: eaten without being planned, given away, thrown out, or never
-- there, when less was made than planned. They count as used, so they're
-- no longer leftovers to plan.
alter table cooks
  add column removed_servings double precision not null default 0
    check (removed_servings >= 0);
