-- name: CreatePayment :one
insert into payments (customer_id, paid_at, amount, method, note, invoice_id)
values ($1, $2, $3, $4, $5, $6)
returning id;

-- name: DeletePayment :execrows
delete from payments where id = $1;

-- name: GetPayment :one
select p.*, c.name as customer_name from payments p join customers c on c.id = p.customer_id where p.id = $1;

-- name: CountPayments :one
select count(*) from payments p join customers c on c.id = p.customer_id
where (@q::text = '' or c.name ilike '%' || @q || '%')
  and (sqlc.narg(from_date)::date is null or p.paid_at >= sqlc.narg(from_date))
  and (sqlc.narg(to_date)::date is null or p.paid_at <= sqlc.narg(to_date));

-- name: ListPayments :many
select p.id, p.customer_id, p.paid_at, p.amount, p.method, p.note, p.invoice_id, c.name as customer_name
from payments p join customers c on c.id = p.customer_id
where (@q::text = '' or c.name ilike '%' || @q || '%')
  and (sqlc.narg(from_date)::date is null or p.paid_at >= sqlc.narg(from_date))
  and (sqlc.narg(to_date)::date is null or p.paid_at <= sqlc.narg(to_date))
order by p.paid_at desc, p.created_at desc
limit @lim offset @off;

-- name: SumPayments :one
select coalesce(sum(amount), 0)::bigint from payments
where (sqlc.narg(from_date)::date is null or paid_at >= sqlc.narg(from_date))
  and (sqlc.narg(to_date)::date is null or paid_at <= sqlc.narg(to_date));

-- Cariye işlenen faturalar: taslaklar, hatalı/reddedilen (29/105/405) ve iptal edilenler (305) hariç.
-- name: LedgerInvoicesByCustomer :many
select id, customer_id, invoice_number, issue_date, due_date, payable
from invoices
where customer_id = $1 and status in ('SENDING', 'ISSUED') and (provider_status is null or provider_status not in (29, 105, 305, 405))
order by issue_date, created_at;

-- name: LedgerInvoicesAll :many
select id, customer_id, invoice_number, issue_date, due_date, payable
from invoices
where status in ('SENDING', 'ISSUED') and (provider_status is null or provider_status not in (29, 105, 305, 405))
order by issue_date, created_at;

-- name: LedgerPaymentsByCustomer :many
select id, customer_id, paid_at, amount, method, note, invoice_id from payments where customer_id = $1 order by paid_at, created_at;

-- name: LedgerPaymentsAll :many
select id, customer_id, paid_at, amount, method, note, invoice_id from payments order by paid_at, created_at;

-- name: LedgerCustomersAll :many
select id, name, opening_balance, active from customers order by lower(name);
