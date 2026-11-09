-- The dates a staple was marked eaten on, which took its food, or its
-- recipe's ingredients, out of the pantry, as a cook marked done does (see
-- cook_pantry_uses). A date is in it at most once, so marking a staple
-- eaten twice takes its food out once.
create table staple_pantry_uses (
  staple_id uuid not null,
  date date not null,
  household_id uuid not null,
  create_time timestamptz not null default current_timestamp,
  primary key (staple_id, date),
  foreign key (household_id, staple_id) references staples (household_id, id) on delete cascade
);

-- What a staple marked eaten took out of each lot, so un-marking it puts
-- back exactly that, in the same lots, like cook_pantry_deductions.
create table staple_pantry_deductions (
  staple_id uuid not null,
  date date not null,
  household_id uuid not null,
  -- The lot's ID. It isn't a foreign key, as the lot is deleted when the
  -- staple empties it, and recreated with the same ID when it's put back.
  pantry_item_id uuid not null,
  food_id uuid not null,
  grams double precision not null check (grams > 0),
  -- The lot's details, to recreate it if it was emptied.
  expiry_date date,
  opened_date date,
  notes text not null default '',
  storage pantry_storage,
  location text not null default '',
  frozen_date date,
  opened boolean not null default false,
  estimated boolean not null default false,
  label text not null default '',
  primary key (staple_id, date, pantry_item_id),
  foreign key (staple_id, date) references staple_pantry_uses (staple_id, date) on delete cascade,
  foreign key (household_id, food_id) references foods (household_id, id) on delete cascade
);

create index staple_pantry_deductions_household_id_food_id on staple_pantry_deductions (household_id, food_id);
