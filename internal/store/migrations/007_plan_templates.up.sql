-- Plan templates: a plan of some days, independent of dates and saved to
-- plan again from a start date, like a dietitian's sample week or a week
-- the household liked. The template's slots, entries and batches are a
-- document in body (see internal/templates), as a template is edited and
-- applied as a whole. Its recipes are checked when it's saved and when
-- it's applied, rather than by foreign keys, so deleting a recipe a
-- template uses isn't blocked: applying the template then says which
-- entries to fix.
create table plan_templates (
  id uuid primary key default gen_random_uuid(),
  household_id uuid not null references households(id) on delete cascade,
  name text not null,
  -- Where the template comes from, e.g. "Dietitian, week 1", or a URL.
  source text not null default '',
  notes text not null default '',
  -- How many days it plans, from its first.
  days integer not null check (days between 1 and 28),
  body jsonb not null,
  create_time timestamptz not null default current_timestamp,
  update_time timestamptz not null default current_timestamp
);

create unique index plan_templates_household_id_lower_name on plan_templates (household_id, lower(name));
