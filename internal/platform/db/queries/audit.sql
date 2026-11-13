-- name: InsertAudit :exec
insert into audit_log (actor, action, entity, entity_id, summary) values ($1, $2, $3, $4, $5);

-- name: CountAudit :one
select count(*) from audit_log
where (@q::text = '' or actor ilike '%' || @q || '%' or action ilike '%' || @q || '%' or summary ilike '%' || @q || '%');

-- name: ListAudit :many
select id, at, actor, action, entity, entity_id, summary from audit_log
where (@q::text = '' or actor ilike '%' || @q || '%' or action ilike '%' || @q || '%' or summary ilike '%' || @q || '%')
order by at desc, id desc
limit @lim offset @off;
