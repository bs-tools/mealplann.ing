-- The initial schema: households, users, and how users sign in. Users sign
-- in with passkeys, and register with an invitation. MCP clients act on
-- behalf of users with API tokens or OAuth grants.

-- A household shares everything the app stores about food: recipes, plans,
-- the pantry and the shopping list. Every user belongs to one household.
create table households (
  id uuid primary key default gen_random_uuid(),
  -- A name for the household, e.g. "Home". At most 100 characters.
  name text not null,
  create_time timestamptz not null default current_timestamp
);

create table users (
  id uuid primary key default gen_random_uuid(),
  household_id uuid not null references households(id) on delete cascade,
  -- Whether the user is an administrator, who can invite new users.
  is_admin boolean not null default false,
  -- The key of the user's avatar image in the blob store, if they uploaded
  -- one. Each upload gets a new key, so avatar images can be cached forever.
  avatar_key text,
  create_time timestamptz not null default current_timestamp
);

create index users_household_id on users (household_id);

-- The users who sign in with passkeys, which is all of them.
create table passkey_users (
  id uuid primary key references users(id) on delete cascade,
  -- The WebAuthn user handle, identifying the user to authenticators.
  user_handle bytea unique not null,
  name text not null
);

create table passkeys (
  -- The credential ID.
  id bytea primary key,
  user_id uuid not null references users(id) on delete cascade,
  -- The authenticator name, e.g. "iCloud Keychain".
  name text not null,
  -- The PKIX encoded public key.
  public_key bytea not null,
  -- The COSE algorithm identifier.
  algorithm integer not null,
  aaguid bytea not null,
  transports text[] not null,
  backup_eligible boolean not null,
  backed_up boolean not null,
  sign_count bigint not null,
  create_time timestamptz not null default current_timestamp,
  last_used_time timestamptz
);

create index passkeys_user on passkeys (user_id);

create type invitation_kind as enum (
  -- The invitation creates a new user in the household of its creator.
  'new_user',
  -- The invitation adds a passkey to an existing user.
  'add_passkey'
);

create table invitations (
  id uuid primary key default gen_random_uuid(),
  token_sha256 bytea unique not null,
  kind invitation_kind not null,
  created_by uuid not null references users(id) on delete cascade,
  -- The user to add a passkey to, for add_passkey invitations.
  user_id uuid references users(id) on delete cascade,
  note text not null,
  create_time timestamptz not null default current_timestamp,
  expire_time timestamptz not null,
  use_time timestamptz
);

create type passkey_ceremony as enum (
  'registration',
  'login'
);

create table passkey_challenges (
  id uuid primary key default gen_random_uuid(),
  ceremony passkey_ceremony not null,
  challenge bytea not null,
  -- For registrations: the user handle of the new credential, and either the
  -- invitation, the signed in user adding a passkey, or the admin bootstrap.
  user_handle bytea,
  invitation_id uuid references invitations(id) on delete cascade,
  user_id uuid references users(id) on delete cascade,
  bootstrap boolean not null default false,
  expire_time timestamptz not null
);

-- Sessions are looked up by the SHA-256 hash of their token, like
-- invitations and API tokens, so the database doesn't hold tokens that sign
-- users in. Sessions expire while stored, and expired sessions are deleted
-- whenever a session is created.
create table sessions (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id) on delete cascade,
  token_sha256 bytea unique not null,
  expiry timestamptz not null,
  -- The passkey the session was signed in with, so deleting a passkey ends
  -- the sessions it started. It's null for the local administrator, who
  -- signs in without one.
  passkey_id bytea references passkeys(id) on delete set null
);

-- Sessions are found by their user to sign out the user's other sessions.
create index sessions_user_id on sessions (user_id);

-- OAuth clients, such as claude.ai custom connectors, act on behalf of users
-- with OAuth grants. Clients register dynamically without being stored: their
-- client ID encodes their registration.

-- A user's authorization of an OAuth client. Deleting it revokes the
-- client's tokens.
create table oauth_grants (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id) on delete cascade,
  client_id text not null,
  client_name text not null,
  redirect_uri text not null,
  -- The current refresh token. Refresh tokens are rotated on every use.
  refresh_token_sha256 bytea unique not null,
  refresh_expire_time timestamptz not null,
  create_time timestamptz not null default current_timestamp
);

create index oauth_grants_user_id on oauth_grants (user_id);

-- An authorization code, issued when a user approves an OAuth client. Codes
-- are single use and short lived. Used codes are kept until they expire,
-- with the grant they were exchanged for: a code used twice was stolen, so
-- the grant is revoked (RFC 6749 section 4.1.2).
create table oauth_authorization_codes (
  code_sha256 bytea primary key,
  user_id uuid not null references users(id) on delete cascade,
  client_id text not null,
  client_name text not null,
  redirect_uri text not null,
  -- The S256 PKCE code challenge.
  code_challenge text not null,
  expire_time timestamptz not null,
  create_time timestamptz not null default current_timestamp,
  consume_time timestamptz,
  oauth_grant_id uuid references oauth_grants(id) on delete set null
);

-- Refresh tokens that were rotated out, until they would have expired. A
-- rotated-out refresh token used again was stolen, so its grant is revoked
-- (RFC 9700 section 4.14.2).
create table oauth_used_refresh_tokens (
  refresh_token_sha256 bytea primary key,
  oauth_grant_id uuid not null references oauth_grants(id) on delete cascade,
  expire_time timestamptz not null
);

create index oauth_used_refresh_tokens_oauth_grant_id on oauth_used_refresh_tokens (oauth_grant_id);

-- API tokens authenticate MCP and other API clients. OAuth access tokens are
-- API tokens belonging to a grant, and expire.
create table api_tokens (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id) on delete cascade,
  name text not null,
  secret_sha256 bytea unique not null,
  create_time timestamptz not null default current_timestamp,
  last_used_time timestamptz,
  oauth_grant_id uuid references oauth_grants(id) on delete cascade,
  expire_time timestamptz
);

create index api_tokens_oauth_grant_id on api_tokens (oauth_grant_id);
