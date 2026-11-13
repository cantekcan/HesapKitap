-- name: CreateAttachment :one
insert into attachments (owner_type, owner_id, filename, content_type, size, data, uploaded_by)
values ($1, $2, $3, $4, $5, $6, $7) returning id;

-- name: GetAttachment :one
select id, owner_type, owner_id, filename, content_type, size, data, uploaded_by, created_at from attachments where id = $1;

-- name: ListAttachments :many
select id, owner_type, owner_id, filename, content_type, size, uploaded_by, created_at
from attachments where owner_type = $1 and owner_id = $2 order by created_at;

-- name: DeleteAttachment :one
delete from attachments where id = $1 returning owner_type, owner_id;

-- name: CountAttachments :many
select owner_id, count(*)::int as n from attachments where owner_type = @owner_type and owner_id = any(@owner_ids::uuid[]) group by owner_id;
