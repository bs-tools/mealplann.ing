alter table shopping_items drop column restock;

alter table foods
  drop constraint foods_keep_in_stock_not_always,
  drop constraint foods_keep_in_stock_unit,
  drop column keep_in_stock_unit,
  drop column keep_in_stock_amount;
