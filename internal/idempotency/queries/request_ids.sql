-- name: DeleteExpiredRequestIDs :exec
-- Request IDs are kept a day.
delete from request_ids where create_time < now() - interval '1 day';

-- name: StartRequest :execrows
-- Records a request as running, unless its ID is taken.
insert into request_ids (household_id, request_id, method)
values (@household_id, @request_id, @method)
on conflict (household_id, request_id) do nothing;

-- name: GetRequest :one
select * from request_ids
where household_id = @household_id and request_id = @request_id;

-- name: FinishRequest :exec
update request_ids set response = @response
where household_id = @household_id and request_id = @request_id;

-- name: DeleteRequest :exec
-- Forgets a failed request, so a retry runs it again.
delete from request_ids
where household_id = @household_id and request_id = @request_id;
