-- name: CreateHousehold :one
insert into households (name) values ($1) returning *;

-- name: GetHousehold :one
select * from households where id = $1;

-- name: GetUserHouseholdID :one
select household_id from users where id = $1;

-- name: RenameHousehold :one
update households set name = $2 where id = $1 returning *;

-- name: ListHouseholdMembers :many
select p.id, p.name, u.avatar_key from passkey_users p
join users u on u.id = p.id
where u.household_id = $1
order by u.create_time, u.id;
