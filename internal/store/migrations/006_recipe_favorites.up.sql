-- Whether the household marked the recipe as a favorite. Suggestions score
-- favorites higher, so they come up more often. Recipes start as not
-- favorites.
alter table recipes add column favorite boolean not null default false;
