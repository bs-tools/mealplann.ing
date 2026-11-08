-- Where a lot is kept, which sets how long it keeps: a freezer keeps food
-- for the food's freezer_shelf_life_days from the day it was frozen,
-- however soon its pack expires. Null if not said, which keeps as the
-- pantry or fridge does.
create type pantry_storage as enum (
  'pantry',
  'fridge',
  'freezer'
);

alter table pantry_items
  add column storage pantry_storage,
  -- Where in the home, e.g. "chest freezer" or "garage shelf", to find it
  -- and tell lots apart.
  add column location text not null default '',
  -- The day it was frozen, for lots in the freezer.
  add column frozen_date date,
  -- Whether it's been opened. opened_date is the day, null if it isn't
  -- known, in which case the opened shelf life can't be counted.
  add column opened boolean not null default false,
  -- Whether the amount is a guess, like a partly used opaque bag in a
  -- photo, for the user to check.
  add column estimated boolean not null default false,
  -- The brand or label of the lot, e.g. "Sriracha", when lots of one food
  -- differ.
  add column label text not null default '';

update pantry_items set opened = true where opened_date is not null;

-- What a cook took out of a lot keeps the lot's details too, to recreate
-- it as it was if the cook emptied it.
alter table cook_pantry_deductions
  add column storage pantry_storage,
  add column location text not null default '',
  add column frozen_date date,
  add column opened boolean not null default false,
  add column estimated boolean not null default false,
  add column label text not null default '';

update cook_pantry_deductions set opened = true where opened_date is not null;

-- How many days the food keeps frozen, from the day it's frozen. Null if
-- unknown.
alter table foods
  add column freezer_shelf_life_days integer check (freezer_shelf_life_days > 0);
