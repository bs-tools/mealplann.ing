-- The responses of create requests with a request ID, so a retry with the
-- same ID, like an agent's after a timeout, returns the first response
-- instead of creating again. Rows are kept a day.
create table request_ids (
  household_id uuid not null references households(id) on delete cascade,
  -- The ID the caller chose for the request.
  request_id text not null,
  -- The method the request was for, like "CreateFood", so the same ID for
  -- another method is refused rather than answered with the wrong response.
  method text not null,
  -- The response, as protobuf, null while the request runs.
  response bytea,
  create_time timestamptz not null default current_timestamp,
  primary key (household_id, request_id)
);

-- Expired rows are deleted by time.
create index request_ids_create_time on request_ids (create_time);
