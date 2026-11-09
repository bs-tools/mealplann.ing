-- A food can also always show in milliliters, whatever the user's units,
-- like a 400 ml can of coconut milk.
alter table foods
  drop constraint foods_measure_check,
  add constraint foods_measure_check check (measure in ('weight', 'volume', 'pieces', 'milliliters'));
