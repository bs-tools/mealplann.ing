-- The least of a food the household wants to have, like a jar of peanut
-- butter, whether or not the plan uses it: shopping lists buy more when
-- the pantry has less, beyond what's planned. Null when it isn't kept in
-- stock. The amount is in the unit, converted to grams with the food's
-- weights when it's needed, so changing them changes it too.
alter table foods
  add column keep_in_stock_amount double precision check (keep_in_stock_amount > 0),
  add column keep_in_stock_unit amount_unit,
  add constraint foods_keep_in_stock_unit check ((keep_in_stock_amount is null) = (keep_in_stock_unit is null)),
  -- A food always in stock isn't tracked by weight, so it can't be kept
  -- above an amount.
  add constraint foods_keep_in_stock_not_always check (not (always_in_stock and keep_in_stock_amount is not null));

-- Whether a shopping list's item is bought to keep its food in stock, for
-- some or all of it.
alter table shopping_items
  add column restock boolean not null default false;
