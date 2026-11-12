-- name: UpsertCalendarFeed :one
-- Creates the user's calendar feed, replacing any old one, whose URL then
-- stops working.
insert into calendar_feeds (user_id, secret_sha256)
values (@user_id, @secret_sha256)
on conflict (user_id) do update
set secret_sha256 = excluded.secret_sha256, create_time = now(), last_used_time = null
returning *;

-- name: GetCalendarFeed :one
select * from calendar_feeds where user_id = @user_id;

-- name: DeleteCalendarFeed :execrows
delete from calendar_feeds where user_id = @user_id;

-- name: UseCalendarFeed :one
-- Returns the user of the calendar feed with the secret, recording that
-- it was read.
update calendar_feeds
set last_used_time = now()
where secret_sha256 = @secret_sha256
returning user_id;
