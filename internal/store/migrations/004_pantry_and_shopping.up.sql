-- The pantry and the shopping list: the stores a household shops at, the
-- foods it has in stock, what cooks marked done took out of stock, and its
-- shopping lists, generated from the plan less what's in stock.

-- A store the household shops at, like the main supermarket or a bulk
-- store. The first, by position, is the main store, where foods without a
-- preferred store are bought.
create table stores (
  id uuid primary key default gen_random_uuid(),
  household_id uuid not null references households(id) on delete cascade,
  -- At most 100 characters, unique in the household ignoring case.
  name text not null,
  -- The store's place in the order of stores, from 0. It isn't unique, so
  -- that reordering doesn't need to avoid collisions: ties are ordered by
  -- creation.
  position integer not null default 0 check (position >= 0),
  create_time timestamptz not null default current_timestamp,
  update_time timestamptz not null default current_timestamp,
  unique (household_id, id)
);

create unique index stores_household_id_lower_name on stores (household_id, lower(name));

-- The store a food is always bought at, null for the main store. Deleting
-- the store moves its foods back to the main store.
alter table foods add column preferred_store_id uuid;
alter table foods add constraint foods_household_id_preferred_store_id_fkey
  foreign key (household_id, preferred_store_id) references stores (household_id, id)
  on delete set null (preferred_store_id);
-- Whether the household always has the food, like salt, spices or oil, so
-- the pantry doesn't track it by weight and the shopping list leaves it
-- out.
alter table foods add column always_in_stock boolean not null default false;

-- A lot of a food in stock: what was bought or added together, with one
-- expiry date. A food can have several lots, so the oldest is used first
-- and what's close to its expiry can be planned for.
create table pantry_items (
  id uuid primary key default gen_random_uuid(),
  household_id uuid not null references households(id) on delete cascade,
  food_id uuid not null,
  -- The amount in stock, converted from pieces or milliliters with the
  -- food's weights. Lots used up by cooks are deleted.
  grams double precision not null check (grams > 0),
  -- The date it expires, unopened, null if it doesn't or isn't known.
  expiry_date date,
  -- The date it was opened, null if it's unopened. The food's
  -- opened_shelf_life_days counts from it.
  opened_date date,
  notes text not null default '',
  create_time timestamptz not null default current_timestamp,
  update_time timestamptz not null default current_timestamp,
  -- Deleting a food deletes its stock.
  foreign key (household_id, food_id) references foods (household_id, id) on delete cascade
);

create index pantry_items_household_id_food_id on pantry_items (household_id, food_id);

-- The cooks marked done whose ingredients were taken out of the pantry. A
-- cook is in it at most once, so marking a cook done twice, even at the
-- same time on two phones, takes its ingredients out once.
create table cook_pantry_uses (
  cook_id uuid primary key,
  household_id uuid not null,
  create_time timestamptz not null default current_timestamp,
  foreign key (household_id, cook_id) references cooks (household_id, id) on delete cascade
);

-- What a cook marked done took out of each lot, so un-marking it puts back
-- exactly that, in the same lots, even ones it emptied and deleted.
create table cook_pantry_deductions (
  cook_id uuid not null references cook_pantry_uses (cook_id) on delete cascade,
  household_id uuid not null,
  -- The lot's ID. It isn't a foreign key, as the lot is deleted when the
  -- cook empties it, and recreated with the same ID when it's put back.
  pantry_item_id uuid not null,
  food_id uuid not null,
  grams double precision not null check (grams > 0),
  -- The lot's dates and notes, to recreate it if it was emptied.
  expiry_date date,
  opened_date date,
  notes text not null default '',
  primary key (cook_id, pantry_item_id),
  foreign key (household_id, food_id) references foods (household_id, id) on delete cascade
);

create index cook_pantry_deductions_household_id_food_id on cook_pantry_deductions (household_id, food_id);

-- A shopping list for the meals of a range of dates, shopped for on its
-- first date.
create table shopping_lists (
  id uuid primary key default gen_random_uuid(),
  household_id uuid not null references households(id) on delete cascade,
  start_date date not null,
  end_date date not null,
  -- When the shopping was done and the checked items went into the pantry,
  -- null until then.
  complete_time timestamptz,
  create_time timestamptz not null default current_timestamp,
  update_time timestamptz not null default current_timestamp,
  check (start_date <= end_date),
  unique (household_id, id)
);

create index shopping_lists_household_id_start_date on shopping_lists (household_id, start_date);

-- An item of a shopping list: generated from the plan, or added by hand.
-- Generated items are replaced when the list is regenerated, keeping the
-- checks of the same foods; items added by hand are kept.
create table shopping_items (
  id uuid primary key default gen_random_uuid(),
  household_id uuid not null,
  list_id uuid not null,
  -- The food, null for an item of free text. Deleting the food keeps the
  -- item, by name.
  food_id uuid,
  -- The food's name when the item was made, or the free text.
  name text not null,
  -- The amount to buy, rounded up to whole packs or pieces, 0 if unknown.
  grams double precision not null default 0 check (grams >= 0),
  -- The amount the plan needs beyond what the pantry has, before
  -- rounding.
  needed_grams double precision not null default 0 check (needed_grams >= 0),
  -- The amount of the pantry the plan uses.
  pantry_grams double precision not null default 0 check (pantry_grams >= 0),
  -- The number of packs to buy, 0 if the food has no pack size.
  packages integer not null default 0 check (packages >= 0),
  -- The amount for people, e.g. '2 × 400 g' or '3 pieces (~450 g)'.
  amount_text text not null default '',
  -- The store to buy it at, null for the main store.
  store_id uuid,
  store_section store_section,
  -- The first date it's needed, null for items added by hand.
  needed_by date,
  -- Whether it won't keep from the shop until it's needed, so it should
  -- be bought on a later trip.
  top_up boolean not null default false,
  -- Why it should be bought later, or what's missing to weigh it.
  warning text not null default '',
  checked boolean not null default false,
  -- The user who checked it, and when.
  checked_by uuid references users(id) on delete set null,
  check_time timestamptz,
  note text not null default '',
  -- Whether it was added by hand.
  manual boolean not null default false,
  create_time timestamptz not null default current_timestamp,
  update_time timestamptz not null default current_timestamp,
  foreign key (household_id, list_id) references shopping_lists (household_id, id) on delete cascade,
  foreign key (household_id, food_id) references foods (household_id, id) on delete set null (food_id),
  foreign key (household_id, store_id) references stores (household_id, id) on delete set null (store_id)
);

create index shopping_items_list_id on shopping_items (list_id);
create index shopping_items_household_id_food_id on shopping_items (household_id, food_id);
create index shopping_items_household_id_store_id on shopping_items (household_id, store_id);
