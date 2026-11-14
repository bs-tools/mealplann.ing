-- The foods an ingredient can be made with instead of its own, in the same
-- amount, like extra firm tofu for firm: the recipe is cooked with
-- whichever is in the pantry. They aren't foreign keys, so a food can be
-- deleted while it's only an alternative: the ones that no longer exist
-- are left out when the ingredients are read.
alter table recipe_ingredients
  add column alternative_food_ids uuid[] not null default '{}';
