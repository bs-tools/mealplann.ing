-- recipe_grams is the weight of a recipe's ingredients that aren't
-- optional, as the recipe's nutrition weighs them: foods in grams, or in
-- milliliters, pieces or packages converted with the food's density, piece
-- weight or package weight, leaving out what can't be weighed. Components
-- aren't counted, which only matters for a component that has components
-- of its own.
create function recipe_grams(recipe uuid) returns double precision
language sql stable as $$
  select coalesce(sum(case ri.unit
    when 'grams' then ri.amount
    when 'milliliters' then ri.amount * f.grams_per_milliliter
    when 'pieces' then ri.amount * f.grams_per_piece
    when 'packages' then ri.amount * f.package_grams
  end), 0)
  from recipe_ingredients ri
  join foods f on f.id = ri.food_id
  where ri.recipe_id = recipe and not ri.optional
$$;

-- component_servings converts an amount of a component, a recipe used as an
-- ingredient of another, to servings of it, as the recipe does: an amount
-- without a unit is in servings already; one in the unit the component's
-- recipe says it makes is converted with its yield; one in grams without
-- that falls back to the weight of its ingredients; otherwise it's 0, as
-- it can't be converted. Cooks of the recipe using it need these servings
-- from the cook that supplies it.
create function component_servings(amount double precision, unit amount_unit, component uuid) returns double precision
language sql stable as $$
  select case
    when unit is null then amount
    when r.yield_amount > 0 and r.yield_unit = unit then amount / r.yield_amount * greatest(r.servings, 1)
    when unit = 'grams' then coalesce(amount / nullif(recipe_grams(r.id), 0) * greatest(r.servings, 1), 0)
    else 0
  end
  from recipes r
  where r.id = component
$$;
