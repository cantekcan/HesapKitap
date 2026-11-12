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
  and (@status::text = '' or i.status = @status);

-- name: ListInvoices :many
select i.id, i.status, i.customer_id, i.issue_date, i.due_date, i.payable, c.name as customer_name
from invoices i join customers c on c.id = i.customer_id
where (@q::text = '' or c.name ilike '%' || @q || '%')
  and (@status::text = '' or i.status = @status)
order by i.issue_date desc, i.created_at desc
limit @lim offset @off;
