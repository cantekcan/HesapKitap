-- name: GetInvoice :one
select i.*, c.name as customer_name
from invoices i join customers c on c.id = i.customer_id
where i.id = $1;

-- name: ListInvoiceLines :many
select * from invoice_lines where invoice_id = $1 order by position;

-- name: CreateInvoice :one
insert into invoices (customer_id, issue_date, due_date, notes, payable)
values ($1, $2, $3, $4, $5)
returning id;

-- name: UpdateDraftInvoice :execrows
update invoices set customer_id = $2, issue_date = $3, due_date = $4, notes = $5, payable = $6, updated_at = now()
where id = $1 and status = 'DRAFT';

-- name: DeleteDraftInvoice :execrows
delete from invoices where id = $1 and status = 'DRAFT';

-- name: DeleteInvoiceLines :exec
delete from invoice_lines where invoice_id = $1;

-- name: InsertInvoiceLine :exec
insert into invoice_lines (invoice_id, position, product_id, item_name, unit, quantity_milli, unit_price, discount, tax_percent)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: CountInvoices :one
select count(*) from invoices i join customers c on c.id = i.customer_id
where (@q::text = '' or c.name ilike '%' || @q || '%')
  and (@status::text = '' or i.status = @status)
  and (@kind::text = '' or i.kind = @kind);

-- name: ListInvoices :many
select i.id, i.status, i.customer_id, i.issue_date, i.due_date, i.payable, c.name as customer_name
from invoices i join customers c on c.id = i.customer_id
where (@q::text = '' or c.name ilike '%' || @q || '%')
  and (@status::text = '' or i.status = @status)
  and (@kind::text = '' or i.kind = @kind)
order by i.issue_date desc, i.created_at desc
limit @lim offset @off;

-- name: BeginInvoiceSend :execrows
-- Taslağı gönderim aşamasına alır. Yalnızca DRAFT satır etkilenir; çift tıklama ikinci kez kuyruğa yazmaz.
update invoices set
    status = 'SENDING', kind = $2, scenario = $3, ettn = $4, target_alias = $5, recipient = $6,
    last_error = '', updated_at = now()
where id = $1 and status = 'DRAFT';

-- name: RecordInvoiceResult :exec
update invoices set
    status = sqlc.arg(status), provider_status = sqlc.arg(provider_status),
    invoice_number = case when sqlc.arg(invoice_number)::text <> '' then sqlc.arg(invoice_number)::text else invoice_number end,
    last_error = sqlc.arg(last_error),
    sent_at = coalesce(sent_at, case when sqlc.arg(status) = 'ISSUED' then now() end), updated_at = now()
where id = sqlc.arg(id);

-- name: RevertInvoiceToDraft :exec
-- Sağlayıcı faturayı kesin olarak reddetti (oluşmadı): kullanıcı düzeltip yeniden deneyebilsin.
update invoices set status = 'DRAFT', kind = null, scenario = '', ettn = null, target_alias = '', recipient = null,
    last_error = $2, updated_at = now()
where id = $1 and status = 'SENDING';

-- name: ListPendingInvoices :many
select id from invoices where status = 'ISSUED' and (provider_status is null or provider_status not in (205, 305, 405, 105, 29))
order by updated_at limit $1;

-- name: InvoiceCountsByKind :many
-- Pano kutucukları ve liste sekmeleri: tür (taslakta boş), durum ve "bu ay mı" kırılımında fatura sayısı.
select coalesce(kind, '')::text as kind, status, (issue_date >= @month_start::date) as this_month, count(*) as n
from invoices
group by 1, 2, 3;
