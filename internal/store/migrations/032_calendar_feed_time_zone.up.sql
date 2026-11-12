-- The time zone of a calendar feed, an IANA name like "America/Vancouver",
-- which the plan's local times are in: calendar apps place events by it,
-- not their own. Feeds made before default to UTC until it's set.
alter table calendar_feeds add column time_zone text not null default 'UTC';
