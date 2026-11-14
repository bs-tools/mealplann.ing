-- The recipes a component ingredient can be made with instead of its own,
-- like quick-cooked beans for soaked ones: a cook of either can supply it.
-- Like the food alternatives, they aren't foreign keys, and those that no
-- longer exist are left out when the ingredients are read.
alter table recipe_ingredients
  add column alternative_recipe_ids uuid[] not null default '{}';
