-- name: CreateTemplate :one
insert into plan_templates (id, household_id, name, source, notes, days, body)
values (@id, @household_id, @name, @source, @notes, @days, @body)
returning *;

-- name: GetTemplate :one
select * from plan_templates where id = @id and household_id = @household_id;

-- name: ListTemplates :many
select * from plan_templates
where household_id = @household_id
order by lower(name), id;

-- name: UpdateTemplate :one
update plan_templates set
  name = @name,
  source = @source,
  notes = @notes,
  days = @days,
  body = @body,
  update_time = now()
where id = @id and household_id = @household_id
returning *;

-- name: DeleteTemplate :execrows
delete from plan_templates where id = @id and household_id = @household_id;
