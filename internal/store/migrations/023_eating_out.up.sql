-- The eating times people eat out at on a date, like lunch at a restaurant
-- or dinner at a friend's: nothing is planned there, so meal plans and
-- suggestions leave them out, and their staples are left out of the day's
-- nutrition and shopping, as if skipped.
create table eating_out (
  household_id uuid not null,
  person_id uuid not null,
  eating_time_id uuid not null,
  date date not null,
  -- Where, or anything else to remember, e.g. "Sam's birthday".
  notes text not null default '',
  create_time timestamptz not null default current_timestamp,
  primary key (person_id, eating_time_id, date),
  foreign key (household_id, person_id) references people (household_id, id) on delete cascade,
  foreign key (person_id, eating_time_id) references eating_times (person_id, id) on delete cascade
);

create index eating_out_household_id_date on eating_out (household_id, date);
