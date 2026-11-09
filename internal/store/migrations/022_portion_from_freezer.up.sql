-- Whether a portion of a cook is eaten from its frozen servings, taken out
-- of the freezer (see PlanService.TakeFromFreezer), whatever its date.
-- Portions eaten after the recipe's fridge days come from the freezer too,
-- without it, as the rest of the cook won't keep in the fridge.
alter table portions add column from_freezer boolean not null default false;
