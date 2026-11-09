-- How each user reads amounts: in metric units, or in US customary ones,
-- like cups and ounces. Amounts are stored in metric either way.
alter table users
  add column unit_system text not null default 'metric'
    check (unit_system in ('metric', 'imperial'));

-- How a food's amounts are shown to people: by weight, by volume, like a
-- teaspoon of cumin or a tablespoon of oil, or in pieces. Null to choose
-- automatically, by the amount.
alter table foods
  add column measure text check (measure in ('weight', 'volume', 'pieces'));
