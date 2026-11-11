-- The recipes an item of a shopping list is bought for, as a JSON array of
-- {"id", "name"}, so the list can link to them: those of the meals it's
-- short for, empty for items added by hand or bought for foods eaten
-- without a recipe. Names are kept as they were when the list was made.
alter table shopping_items add column recipes jsonb not null default '[]';
