-- People and plans: the people a household plans meals for, with their
-- eating patterns, rules and targets, and the plan of what they eat: cooks
-- of recipes, the portions people eat of them or of foods, and staples, the
-- portions they eat regularly.

-- The target of the composite foreign key that keeps a person's user in the
-- person's household.
alter table users add constraint users_household_id_id unique (household_id, id);

-- The meals a household cooks to share. A null household_meal is none.
create type household_meal as enum (
  'breakfast',
  'lunch',
  'dinner'
);

create type eating_time_kind as enum (
  'main',
  'small'
);

-- The nutrients, named after the fields of the API's Nutrients message.
create type nutrient as enum (
  'energy_kcal',
  'protein_grams',
  'carbohydrate_grams',
  'sugars_grams',
  'fat_grams',
  'fiber_grams',
  'vitamin_b12_micrograms',
  'iron_milligrams',
  'calcium_milligrams',
  'vitamin_d_micrograms',
  'iodine_micrograms',
  'zinc_milligrams',
  'omega3_ala_grams',
  'folate_micrograms',
  'vitamin_a_micrograms',
  'vitamin_e_milligrams',
  'vitamin_k_micrograms',
  'copper_milligrams',
  'magnesium_milligrams',
  'sodium_milligrams',
  'potassium_milligrams'
);

-- A person the household plans meals for, with the rules every one of
-- their eating times is checked against. The rules are entered from the
-- person's care team, and null when there's no rule.
create table people (
  id uuid primary key default gen_random_uuid(),
  household_id uuid not null references households(id) on delete cascade,
  -- At most 100 characters.
  name text not null,
  -- The household's user the person is, if any. Deleting the user keeps
  -- the person, without the user.
  user_id uuid,
  max_portion_grams double precision check (max_portion_grams > 0),
  min_protein_grams double precision check (min_protein_grams >= 0),
  max_carbohydrate_grams double precision check (max_carbohydrate_grams >= 0),
  -- The least (protein + fat) / carbohydrate of an eating time.
  min_pairing_ratio double precision check (min_pairing_ratio >= 0),
  max_sugars_grams double precision check (max_sugars_grams >= 0),
  warn_fast_carbs boolean not null default false,
  notes text not null default '',
  create_time timestamptz not null default current_timestamp,
  update_time timestamptz not null default current_timestamp,
  unique (household_id, id),
  foreign key (household_id, user_id) references users (household_id, id) on delete set null (user_id)
);

-- A user is at most one person.
create unique index people_household_id_user_id on people (household_id, user_id);

-- A person's eating times, in order. Updating a person keeps the eating
-- times with the same IDs, and deletes the others, with their portions and
-- staples.
create table eating_times (
  id uuid primary key,
  household_id uuid not null,
  person_id uuid not null,
  -- The eating time's index in the person's day, from 0. It isn't unique,
  -- so that reordering doesn't need to avoid collisions.
  position integer not null check (position >= 0),
  -- E.g. 'Breakfast' or 'Mid-morning'.
  name text not null,
  kind eating_time_kind not null,
  -- The usual time of day, 'HH:MM', or '' if unknown.
  time_of_day text not null default '',
  -- The shared meal the eating time eats from, null if none. The service
  -- keeps it to one eating time per meal and person.
  meal household_meal,
  -- The target of portions' and staples' foreign keys, which keep their
  -- eating time the person's.
  unique (person_id, id),
  foreign key (household_id, person_id) references people (household_id, id) on delete cascade
);

create index eating_times_household_id on eating_times (household_id);

-- A person's daily targets, replaced as a whole when the person changes.
create table person_targets (
  household_id uuid not null,
  person_id uuid not null,
  nutrient nutrient not null,
  -- In the nutrient's unit. At least one is set.
  min_per_day double precision check (min_per_day >= 0),
  max_per_day double precision check (max_per_day >= 0),
  primary key (person_id, nutrient),
  check (min_per_day is not null or max_per_day is not null),
  check (min_per_day <= max_per_day),
  foreign key (household_id, person_id) references people (household_id, id) on delete cascade
);

create index person_targets_household_id on person_targets (household_id);

-- A recipe made on a date for a number of servings. Recipes can't be
-- deleted while cooks use them.
create table cooks (
  id uuid primary key default gen_random_uuid(),
  household_id uuid not null references households(id) on delete cascade,
  recipe_id uuid not null,
  date date not null,
  -- The recipe is scaled by servings / recipes.servings.
  servings double precision not null check (servings > 0),
  meal household_meal,
  notes text not null default '',
  -- Whether it's been cooked, which will take its ingredients out of the
  -- pantry.
  done boolean not null default false,
  create_time timestamptz not null default current_timestamp,
  update_time timestamptz not null default current_timestamp,
  unique (household_id, id),
  foreign key (household_id, recipe_id) references recipes (household_id, id)
);

create index cooks_household_id_date on cooks (household_id, date);
create index cooks_household_id_recipe_id on cooks (household_id, recipe_id);

-- What a person eats at one of their eating times on a date: a share of a
-- cook, in servings, or an amount of a food.
create table portions (
  id uuid primary key default gen_random_uuid(),
  household_id uuid not null references households(id) on delete cascade,
  person_id uuid not null,
  eating_time_id uuid not null,
  date date not null,
  cook_id uuid,
  servings double precision check (servings > 0),
  food_id uuid,
  amount double precision check (amount > 0),
  unit amount_unit,
  notes text not null default '',
  create_time timestamptz not null default current_timestamp,
  update_time timestamptz not null default current_timestamp,
  check (
    (cook_id is not null and servings is not null and food_id is null and amount is null and unit is null)
    or (cook_id is null and servings is null and food_id is not null and amount is not null and unit is not null)
  ),
  foreign key (household_id, person_id) references people (household_id, id) on delete cascade,
  foreign key (person_id, eating_time_id) references eating_times (person_id, id) on delete cascade,
  foreign key (household_id, cook_id) references cooks (household_id, id) on delete cascade,
  -- Foods eaten in portions can't be deleted.
  foreign key (household_id, food_id) references foods (household_id, id)
);

create index portions_household_id_date on portions (household_id, date);
create index portions_cook_id on portions (cook_id);
create index portions_household_id_food_id on portions (household_id, food_id);
create index portions_eating_time_id on portions (eating_time_id);

-- A portion a person eats regularly, on some weekdays, of servings of a
-- recipe or an amount of a food. Staples aren't copied into portions:
-- reading a plan expands them on the dates they're on.
create table staples (
  id uuid primary key default gen_random_uuid(),
  household_id uuid not null references households(id) on delete cascade,
  person_id uuid not null,
  eating_time_id uuid not null,
  recipe_id uuid,
  servings double precision check (servings > 0),
  food_id uuid,
  amount double precision check (amount > 0),
  unit amount_unit,
  -- ISO weekdays, 1 for Monday to 7 for Sunday. Empty for every day.
  weekdays smallint[] not null default '{}',
  -- The first and last dates, null if unbounded.
  start_date date,
  end_date date,
  notes text not null default '',
  create_time timestamptz not null default current_timestamp,
  update_time timestamptz not null default current_timestamp,
  check (
    (recipe_id is not null and servings is not null and food_id is null and amount is null and unit is null)
    or (recipe_id is null and servings is null and food_id is not null and amount is not null and unit is not null)
  ),
  check (start_date <= end_date),
  unique (household_id, id),
  foreign key (household_id, person_id) references people (household_id, id) on delete cascade,
  foreign key (person_id, eating_time_id) references eating_times (person_id, id) on delete cascade,
  foreign key (household_id, recipe_id) references recipes (household_id, id),
  foreign key (household_id, food_id) references foods (household_id, id)
);

create index staples_household_id on staples (household_id);
create index staples_household_id_recipe_id on staples (household_id, recipe_id);
create index staples_household_id_food_id on staples (household_id, food_id);
create index staples_eating_time_id on staples (eating_time_id);

-- The dates a staple is skipped on.
create table staple_skips (
  household_id uuid not null,
  staple_id uuid not null,
  date date not null,
  primary key (staple_id, date),
  foreign key (household_id, staple_id) references staples (household_id, id) on delete cascade
);
