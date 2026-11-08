-- Leftovers stored in the freezer, to eat after they stop keeping in the
-- fridge.

-- How many days a recipe's leftovers keep frozen, e.g. 90. Null if
-- unknown, so frozen leftovers aren't checked against a limit.
alter table recipes add column freezer_days integer check (freezer_days between 1 and 365);

-- How many of a cook's servings are frozen, on the day it's cooked. Its
-- portions eaten after the recipe's fridge_days come from them, and those
-- not planned yet are in the freezer, to plan later.
alter table cooks add column frozen_servings double precision not null default 0
  check (frozen_servings >= 0 and frozen_servings <= servings);
