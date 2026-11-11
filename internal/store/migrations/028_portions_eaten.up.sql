-- Whether a portion was eaten, marked as the day goes, so the day shows
-- what's done and what's left. Marking a portion of a food eaten takes the
-- food out of the pantry, as a staple marked eaten does; a share of a cook
-- took its ingredients when the cook was marked done.
alter table portions add column eaten boolean not null default false;

-- The portions of foods marked eaten, which took their food out of the
-- pantry, like staple_pantry_uses. A portion is in it at most once, so
-- marking it eaten twice takes its food out once.
create table portion_pantry_uses (
  portion_id uuid primary key references portions (id) on delete cascade,
  household_id uuid not null references households (id) on delete cascade,
  create_time timestamptz not null default current_timestamp
);

-- What a portion marked eaten took out of each lot, so un-marking it puts
-- back exactly that, in the same lots, like staple_pantry_deductions.
create table portion_pantry_deductions (
  portion_id uuid not null references portion_pantry_uses (portion_id) on delete cascade,
  household_id uuid not null,
  -- The lot's ID. It isn't a foreign key, as the lot is deleted when the
  -- portion empties it, and recreated with the same ID when it's put back.
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
  primary key (portion_id, pantry_item_id),
  foreign key (household_id, food_id) references foods (household_id, id) on delete cascade
);

create index portion_pantry_deductions_household_id_food_id on portion_pantry_deductions (household_id, food_id);
