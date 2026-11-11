-- The sides a recipe is served with, like rice or quinoa, as a JSON array
-- of {"food_id", "grams"}, grams per serving, in order. They aren't
-- ingredients: a meal plans one of them, or none, as a portion of its own.
alter table recipes add column served_with jsonb not null default '[]';
