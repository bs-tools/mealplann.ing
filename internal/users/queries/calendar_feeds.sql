-- name: UpsertCalendarFeed :one
-- Creates the user's calendar feed, replacing any old one, whose URL then
-- stops working.
insert into calendar_feeds (user_id, secret_sha256, time_zone)
values (@user_id, @secret_sha256, @time_zone)
on conflict (user_id) do update
set secret_sha256 = excluded.secret_sha256, time_zone = excluded.time_zone,
  create_time = now(), last_used_time = null
returning *;

-- name: SetCalendarFeedTimeZone :one
update calendar_feeds set time_zone = @time_zone
where user_id = @user_id
returning *;

-- name: GetCalendarFeed :one
select * from calendar_feeds where user_id = @user_id;

-- name: DeleteCalendarFeed :execrows
delete from calendar_feeds where user_id = @user_id;

-- name: UseCalendarFeed :one
-- Returns the user and time zone of the calendar feed with the secret,
-- recording that it was read.
update calendar_feeds
set last_used_time = now()
where secret_sha256 = @secret_sha256
returning user_id, time_zone;
