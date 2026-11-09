-- How many of a cook's servings were taken without a portion: eaten
-- without being planned, given away or thrown out, like a frozen serving
-- grabbed for a lunch. They count as used, so they're neither unplanned
-- nor in the freezer.
alter table cooks
  add column off_plan_servings double precision not null default 0
    check (off_plan_servings >= 0);
