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

-- Karşılıksız çıkan çek/senet bakiyeye sayılmaz (borç geri açılır).
-- name: LedgerPaymentsByCustomer :many
select id, customer_id, paid_at, amount, method, note, invoice_id from payments p where customer_id = $1
  and not exists (select 1 from cheques c where c.payment_id = p.id and c.status = 'BOUNCED')
order by paid_at, created_at;

-- name: LedgerPaymentsAll :many
select id, customer_id, paid_at, amount, method, note, invoice_id from payments p
where not exists (select 1 from cheques c where c.payment_id = p.id and c.status = 'BOUNCED')
order by paid_at, created_at;

-- name: LedgerCustomersAll :many
select id, name, opening_balance, active from customers order by lower(name);

-- Dönemsel satış raporu: cariye işlenen faturalar ve kalemleri (KDV kırılımı InvoiceCalculator ile hesaplanır).
-- name: ReportInvoices :many
select i.id, i.customer_id, c.name as customer_name, i.invoice_number, i.issue_date, i.due_date, i.payable
from invoices i join customers c on c.id = i.customer_id
where i.status in ('SENDING', 'ISSUED') and (i.provider_status is null or i.provider_status not in (29, 105, 305, 405))
  and i.issue_date >= @from_date::date and i.issue_date <= @to_date::date
order by i.issue_date, i.created_at;

-- name: ReportInvoiceLines :many
select l.invoice_id, l.item_name, l.unit, l.quantity_milli, l.unit_price, l.discount, l.tax_percent
from invoice_lines l join invoices i on i.id = l.invoice_id
where i.status in ('SENDING', 'ISSUED') and (i.provider_status is null or i.provider_status not in (29, 105, 305, 405))
  and i.issue_date >= @from_date::date and i.issue_date <= @to_date::date
order by l.invoice_id, l.position;

-- Dikkat gerektirenler: hatalı, reddedilip taslağa dönen ve 10 dakikadan uzun süredir gönderilemeyen faturalar.
-- name: AttentionInvoices :many
select i.id, i.status, c.name as customer_name, i.invoice_number, i.last_error, i.updated_at, i.payable
from invoices i join customers c on c.id = i.customer_id
where i.status = 'FAILED'
   or (i.status = 'DRAFT' and i.last_error <> '')
   or (i.status = 'SENDING' and i.updated_at < now() - interval '10 minutes')
order by i.updated_at desc
limit 20;
