-- A person's profile, for the plan to fit their body and goals: height,
-- weight, birth year, sex, activity and goal; and their tastes, which the
-- suggestions favor: sweet or savory small meals, how spicy, and foods or
-- dishes they like and dislike. It's JSON, as it's read and written whole
-- with the person, and grows with what the plan learns to use.
alter table people add column profile jsonb not null default '{}';
