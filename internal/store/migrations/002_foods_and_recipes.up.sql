-- Foods and recipes: the foods of USDA FoodData Central, the household's
-- own foods, and its recipes made of them.
--
-- Nutrient columns are named after the fields of the API's Nutrients
-- message, with the amount of food they're in as a suffix, e.g.
-- protein_grams_per_100_grams. They're null when the amount is unknown,
-- which is different from 0: FoodData Central often lacks iodine, vitamin
-- B12 and vitamin D.

-- Trigram indexes find FoodData Central foods by misspelled or partial
-- words. Plain Postgres and Neon both ship pg_trgm, and database owners can
-- create it.
create extension if not exists pg_trgm;

-- The foods of USDA FoodData Central's Foundation Foods and SR Legacy
-- datasets, without the animal categories, imported by `foodplanning
-- import-fdc`. They're shared by all households, which copy the ones they
-- use into foods, so all nutrition math reads one table. Imports upsert by
-- fdc_id, and never delete.
create table fdc_foods (
  fdc_id integer primary key,
  -- The dataset: 'foundation_food' or 'sr_legacy_food'.
  data_type text not null,
  -- E.g. 'Chickpeas (garbanzo beans, bengal gram), mature seeds, raw'.
  description text not null,
  -- The FDC food category, e.g. 'Legumes and Legume Products'.
  category text not null,
  -- The common portions, as a JSON array of objects with the portion's
  -- description, e.g. "1 cup, chopped", the amount of the unit it
  -- describes, e.g. 1, and its weight in grams:
  -- [{"description": "1 medium", "amount": 1, "grams": 110}].
  portions jsonb not null default '[]',
  publication_date date,
  energy_kcal_per_100_grams double precision,
  protein_grams_per_100_grams double precision,
  carbohydrate_grams_per_100_grams double precision,
  sugars_grams_per_100_grams double precision,
  fat_grams_per_100_grams double precision,
  fiber_grams_per_100_grams double precision,
  vitamin_b12_micrograms_per_100_grams double precision,
  iron_milligrams_per_100_grams double precision,
  calcium_milligrams_per_100_grams double precision,
  vitamin_d_micrograms_per_100_grams double precision,
  iodine_micrograms_per_100_grams double precision,
  zinc_milligrams_per_100_grams double precision,
  omega3_ala_grams_per_100_grams double precision,
  folate_micrograms_per_100_grams double precision,
  vitamin_a_micrograms_per_100_grams double precision,
  vitamin_e_milligrams_per_100_grams double precision,
  vitamin_k_micrograms_per_100_grams double precision,
  copper_milligrams_per_100_grams double precision,
  magnesium_milligrams_per_100_grams double precision,
  sodium_milligrams_per_100_grams double precision,
  potassium_milligrams_per_100_grams double precision,
  update_time timestamptz not null default current_timestamp
);

-- Searches match whole words, stemmed ("chickpea" finds "Chickpeas"), and
-- fall back to trigrams for misspellings and partial words.
create index fdc_foods_description_tsvector on fdc_foods using gin (to_tsvector('english', description));
create index fdc_foods_description_trgm on fdc_foods using gin (description gin_trgm_ops);

-- Where a household's food is found in the store, grouping the shopping
-- list. A null store_section is unspecified.
create type store_section as enum (
  'produce',
  'bakery',
  'chilled',
  'frozen',
  'dry_goods',
  'canned',
  'spices',
  'drinks',
  'household',
  'other'
);

-- A household's food: an ingredient, with its nutrients per 100 g, copied
-- from FoodData Central or entered by hand or by an agent from a label.
create table foods (
  id uuid primary key default gen_random_uuid(),
  household_id uuid not null references households(id) on delete cascade,
  -- At most 200 characters, unique in the household ignoring case.
  name text not null,
  -- The FoodData Central food the nutrients were copied from, null for
  -- custom foods. It isn't a foreign key, so a database without the import
  -- can still hold foods copied from another, and FDC IDs never change.
  fdc_id integer,
  energy_kcal_per_100_grams double precision,
  protein_grams_per_100_grams double precision,
  carbohydrate_grams_per_100_grams double precision,
  sugars_grams_per_100_grams double precision,
  fat_grams_per_100_grams double precision,
  fiber_grams_per_100_grams double precision,
  vitamin_b12_micrograms_per_100_grams double precision,
  iron_milligrams_per_100_grams double precision,
  calcium_milligrams_per_100_grams double precision,
  vitamin_d_micrograms_per_100_grams double precision,
  iodine_micrograms_per_100_grams double precision,
  zinc_milligrams_per_100_grams double precision,
  omega3_ala_grams_per_100_grams double precision,
  folate_micrograms_per_100_grams double precision,
  vitamin_a_micrograms_per_100_grams double precision,
  vitamin_e_milligrams_per_100_grams double precision,
  vitamin_k_micrograms_per_100_grams double precision,
  copper_milligrams_per_100_grams double precision,
  magnesium_milligrams_per_100_grams double precision,
  sodium_milligrams_per_100_grams double precision,
  potassium_milligrams_per_100_grams double precision,
  -- The typical weight of one piece, e.g. 150 for an onion, to weigh
  -- amounts in pieces. Null if unknown.
  grams_per_piece double precision check (grams_per_piece > 0),
  -- The density, to weigh amounts in milliliters. Null if unknown.
  grams_per_milliliter double precision check (grams_per_milliliter > 0),
  -- Whether the food's carbohydrate is quickly absorbed, for people with
  -- reactive hypoglycemia. Set by hand, as FDC has no glycemic index.
  fast_carb boolean not null default false,
  store_section store_section,
  -- The typical pack size, to round shopping quantities up. Null if
  -- unknown or sold loose.
  package_grams double precision check (package_grams > 0),
  -- How long the food keeps once bought, and once opened. Null if unknown.
  shelf_life_days integer check (shelf_life_days > 0),
  opened_shelf_life_days integer check (opened_shelf_life_days > 0),
  notes text not null default '',
  create_time timestamptz not null default current_timestamp,
  update_time timestamptz not null default current_timestamp,
  -- The target of composite foreign keys, which keep references to a food
  -- within its household.
  unique (household_id, id)
);

create unique index foods_household_id_lower_name on foods (household_id, lower(name));

-- The unit of an amount of a food. Amounts in milliliters and pieces are
-- weighed with the food's grams_per_milliliter and grams_per_piece.
create type amount_unit as enum (
  'grams',
  'milliliters',
  'pieces'
);

create table recipes (
  id uuid primary key default gen_random_uuid(),
  household_id uuid not null references households(id) on delete cascade,
  name text not null,
  description text not null default '',
  -- How many servings the recipe makes, which nutrition per serving divides
  -- it by.
  servings integer not null check (servings >= 1),
  prep_seconds integer not null default 0 check (prep_seconds >= 0),
  cook_seconds integer not null default 0 check (cook_seconds >= 0),
  -- The steps, in order, without their numbers.
  steps text[] not null default '{}',
  -- Lowercase tags, e.g. 'breakfast' or 'batch-friendly'.
  tags text[] not null default '{}',
  source_url text not null default '',
  -- How many days leftovers keep in the fridge, for planning leftovers.
  -- Null if unknown.
  fridge_days integer check (fridge_days >= 0),
  freezable boolean not null default false,
  notes text not null default '',
  create_time timestamptz not null default current_timestamp,
  update_time timestamptz not null default current_timestamp,
  unique (household_id, id)
);

create index recipes_household_id on recipes (household_id);

-- A recipe's ingredients, in order. They're replaced as a whole when the
-- recipe changes.
create table recipe_ingredients (
  recipe_id uuid not null,
  -- The household of the recipe and the food, which the foreign keys keep
  -- the same, so a recipe can't use another household's food.
  household_id uuid not null,
  -- The ingredient's index in the recipe, from 0.
  position integer not null check (position >= 0),
  food_id uuid not null,
  amount double precision not null check (amount > 0),
  unit amount_unit not null,
  -- How the food is prepared, e.g. 'finely chopped'.
  note text not null default '',
  -- The ingredient as the source recipe wrote it.
  original_text text not null default '',
  -- Optional ingredients, like garnishes, aren't counted in the nutrition.
  optional boolean not null default false,
  primary key (recipe_id, position),
  foreign key (household_id, recipe_id) references recipes (household_id, id) on delete cascade,
  -- Foods used by recipes can't be deleted, until they're removed from the
  -- recipes.
  foreign key (household_id, food_id) references foods (household_id, id)
);

-- Deleting a food looks for the recipes using it.
create index recipe_ingredients_household_id_food_id on recipe_ingredients (household_id, food_id);
