-- Whether the household doesn't want a recipe suggested, though it can
-- still plan it, and the date it's not suggested before, like a recipe they
-- had enough of for a while.
alter table recipes
  add column not_suggested boolean not null default false,
  add column snoozed_until date;
