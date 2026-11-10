-- Whether a cook supplies a component of another from its frozen servings,
-- like beans cooked weeks ago for a chili today: those servings count as
-- taken out of the freezer, rather than as leftovers in the fridge.
alter table cook_supplies add column from_freezer boolean not null default false;
