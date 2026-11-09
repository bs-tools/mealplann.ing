-- The kitchen unit a recipe's ingredient was entered in, like tablespoons,
-- to show it as written. Its amount stays in its base unit, milliliters
-- or grams, which everything computes with. Null if it was entered in its
-- base unit.
alter table recipe_ingredients
  add column kitchen_unit text check (
    kitchen_unit in ('teaspoons', 'tablespoons', 'cups', 'liters', 'kilograms', 'ounces', 'pounds')
  );
