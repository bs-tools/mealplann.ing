-- A user's calendar feed: a secret URL calendar apps subscribe to, which
-- can't sign in, for the cooks, prep and meals of the user's days. Only a
-- hash of the secret is stored, like an API token's, and each user has at
-- most one, made anew to stop the old URL working.
create table calendar_feeds (
  user_id uuid primary key references users(id) on delete cascade,
  secret_sha256 bytea unique not null,
  create_time timestamptz not null default current_timestamp,
  last_used_time timestamptz
);
