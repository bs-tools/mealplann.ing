drop function ingredient_servings(recipe_ingredients, uuid);

alter table recipe_ingredients
  drop column alternative_recipe_units,
  drop column alternative_recipe_amounts,
  drop column alternative_food_units,
  drop column alternative_food_amounts;
