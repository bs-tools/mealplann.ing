-- Whether a cook is to cook any time soon, rather than on its date: one of
-- the recipes picked to buy for and cook whenever suits, like a few dinners
-- for the week. Its date is the day it was picked, the earliest it's
-- cooked, so the shopping list from then buys for it; cooking it moves it
-- to the day it's cooked on.
alter table cooks add column anytime boolean not null default false;
