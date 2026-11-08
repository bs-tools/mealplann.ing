-- Recipes used in other recipes, like soaked and pressure cooked chickpeas
-- in a chili, and the cooks that make them for others.

-- An ingredient is a food, in a unit, or another recipe of the household,
-- a component, in servings, with no unit. Components can't be deleted
-- while recipes use them.
alter table recipe_ingredients alter column food_id drop not null;
alter table recipe_ingredients alter column unit drop not null;
alter table recipe_ingredients add column component_recipe_id uuid;
alter table recipe_ingredients add constraint recipe_ingredients_food_or_component check (
  (food_id is not null and unit is not null and component_recipe_id is null)
  or (food_id is null and unit is null and component_recipe_id is not null)
);
alter table recipe_ingredients add constraint recipe_ingredients_component_recipe_id_fkey
  foreign key (household_id, component_recipe_id) references recipes (household_id, id);
create index recipe_ingredients_component_recipe_id on recipe_ingredients (component_recipe_id)
  where component_recipe_id is not null;

-- How many days before it's cooked a recipe starts, e.g. 1 to soak beans
-- the night before, and what to do then, e.g. "Soak the chickpeas".
alter table recipes add column prep_ahead_days integer not null default 0
  check (prep_ahead_days between 0 and 7);
alter table recipes add column prep_ahead_note text not null default '';

-- Which cook supplies a component of another cook: e.g. Thursday's pot of
-- chickpeas for Friday's chili. One cook can supply several. The servings
-- supplied aren't stored, but computed from the consumer's servings and
-- its recipe's amount of the component, so they follow changes to either.
-- A component of a cook without a supplier is made as part of the cook.
create table cook_supplies (
  household_id uuid not null,
  consumer_cook_id uuid not null,
  component_recipe_id uuid not null,
  supplier_cook_id uuid not null,
  primary key (consumer_cook_id, component_recipe_id),
  foreign key (household_id, consumer_cook_id) references cooks (household_id, id) on delete cascade,
  foreign key (household_id, supplier_cook_id) references cooks (household_id, id) on delete cascade,
  check (consumer_cook_id <> supplier_cook_id)
);

create index cook_supplies_supplier_cook_id on cook_supplies (supplier_cook_id);
