-- What a whole recipe makes, like 1200 g of cooked chickpeas, so recipes
-- using it as a component can give its amount by weight, volume or
-- pieces, converted to servings. Null if not given.
alter table recipes
  add column yield_amount double precision check (yield_amount > 0),
  add column yield_unit amount_unit,
  add constraint recipes_yield_amount_unit check ((yield_amount is null) = (yield_unit is null));

-- A draft recipe is saved unfinished, like one being written or imported
-- from a web page: its ingredients may not be matched to a food yet, and it
-- isn't planned or suggested until it's saved as a recipe.
alter table recipes add column draft boolean not null default false;

-- A component's amount can be in a unit of what its recipe makes, rather
-- than in servings, and a draft's ingredient can be unmatched: neither a
-- food nor a component, only its original text, with an amount if known.
alter table recipe_ingredients
  drop constraint recipe_ingredients_food_or_component,
  add constraint recipe_ingredients_food_or_component check (
    (food_id is not null and unit is not null and component_recipe_id is null)
    or (food_id is null and component_recipe_id is not null)
    or (food_id is null and component_recipe_id is null and original_text <> '')
  ),
  drop constraint recipe_ingredients_amount_check,
  add constraint recipe_ingredients_amount_check check (
    amount > 0 or (amount = 0 and food_id is null and component_recipe_id is null)
  );
