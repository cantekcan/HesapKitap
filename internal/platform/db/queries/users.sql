-- name: CreateUser :one
insert into users (email, password_hash)
values ($1, $2)
returning *;

-- name: GetUserByEmail :one
select * from users where email = $1;

-- name: GetUserByID :one
select * from users where id = $1;

-- name: RecordFailedLogin :exec
-- 5 hatalı denemede hesap 15 dakika kilitlenir ve sayaç sıfırlanır.
update users
set failed_attempts = case when failed_attempts + 1 >= 5 then 0 else failed_attempts + 1 end,
    locked_until    = case when failed_attempts + 1 >= 5 then now() + interval '15 minutes' else locked_until end
where id = $1;

-- name: ResetLoginFailures :exec
update users set failed_attempts = 0, locked_until = null where id = $1;
