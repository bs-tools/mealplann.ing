-- The amounts of an ingredient's alternatives, by their position in
-- alternative_food_ids and alternative_recipe_ids, like 1 package of canned
-- beans for 250 g of a pot of beans, or 400 g of extra firm tofu for a
-- block of firm. An amount of 0, or one missing, is the ingredient's own
-- amount, as before alternatives had amounts. A unit of '' is servings,
-- for a recipe, like a component's.
alter table recipe_ingredients
  add column alternative_food_amounts double precision[] not null default '{}',
  add column alternative_food_units text[] not null default '{}',
  add column alternative_recipe_amounts double precision[] not null default '{}',
  add column alternative_recipe_units text[] not null default '{}';

-- ingredient_servings is the servings of a recipe a component ingredient
-- takes from a cook of it supplying it (see component_servings): of the
-- ingredient's own amount, or of the alternative's, if the recipe is an
-- alternative with an amount of its own.
create function ingredient_servings(ri recipe_ingredients, supplier uuid) returns double precision
language sql stable as $$
  select case
    when coalesce(ri.alternative_recipe_amounts[i], 0) > 0
      then component_servings(ri.alternative_recipe_amounts[i], nullif(ri.alternative_recipe_units[i], '')::amount_unit, supplier)
    else component_servings(ri.amount, ri.unit, supplier)
  end
  from (select array_position(ri.alternative_recipe_ids, supplier) as i) p
$$;
