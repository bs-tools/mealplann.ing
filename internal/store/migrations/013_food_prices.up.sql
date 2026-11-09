-- What the household pays for a food, in Canadian cents per kilogram, as
-- seen in its stores on price_date, to estimate what meals and plans cost.
-- Null if unknown.
alter table foods
  add column price_cents_per_kilogram integer check (price_cents_per_kilogram >= 0),
  add column price_date date;
