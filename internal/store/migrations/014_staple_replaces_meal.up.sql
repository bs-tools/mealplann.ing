-- Whether a staple replaces the shared meal at its eating time, like
-- someone's own breakfast, so the meal isn't planned for its person, or is
-- eaten alongside it, like a cup of tea at breakfast. Off by default, as
-- most staples go with the meal.
alter table staples add column replaces_meal boolean not null default false;
