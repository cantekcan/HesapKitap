-- Çek/senet alınan tahsilat: ödeme ve çek aynı ifadede, atomik yazılır.
-- name: CreateChequePayment :one
with p as (
    insert into payments (customer_id, paid_at, amount, method, note, invoice_id)
    values ($1, $2, $3, $4, $5, $6)
    returning id
)
insert into cheques (payment_id, number, bank, drawer, due_date)
select p.id, $7, $8, $9, $10 from p
returning payment_id;

-- name: ListCheques :many
select c.id, c.payment_id, p.customer_id, cu.name as customer_name, p.method, c.number, c.bank, c.drawer,
       p.paid_at, c.due_date, p.amount, c.status, c.status_date, p.note,
       coalesce((select s.name from supplier_payments sp join suppliers s on s.id = sp.supplier_id where sp.from_cheque_id = c.id), '')::text as endorsed_to
from cheques c
join payments p on p.id = c.payment_id
join customers cu on cu.id = p.customer_id
where (@status::text = '' or c.status = @status)
  and (@q::text = '' or cu.name ilike '%' || @q || '%' or c.number ilike '%' || @q || '%' or c.drawer ilike '%' || @q || '%')
  and (sqlc.narg(from_due)::date is null or c.due_date >= sqlc.narg(from_due))
  and (sqlc.narg(to_due)::date is null or c.due_date <= sqlc.narg(to_due))
order by c.due_date, c.created_at
limit @lim offset @off;

-- name: CountCheques :one
select count(*) from cheques c
join payments p on p.id = c.payment_id
join customers cu on cu.id = p.customer_id
where (@status::text = '' or c.status = @status)
  and (@q::text = '' or cu.name ilike '%' || @q || '%' or c.number ilike '%' || @q || '%' or c.drawer ilike '%' || @q || '%')
  and (sqlc.narg(from_due)::date is null or c.due_date >= sqlc.narg(from_due))
  and (sqlc.narg(to_due)::date is null or c.due_date <= sqlc.narg(to_due));

-- name: GetCheque :one
select c.id, c.payment_id, p.customer_id, cu.name as customer_name, p.method, c.number, c.bank, c.drawer,
       p.paid_at, c.due_date, p.amount, c.status, c.status_date, p.note,
       coalesce((select s.name from supplier_payments sp join suppliers s on s.id = sp.supplier_id where sp.from_cheque_id = c.id), '')::text as endorsed_to
from cheques c
join payments p on p.id = c.payment_id
join customers cu on cu.id = p.customer_id
where c.id = $1;

-- name: SetChequeStatus :execrows
update cheques set status = @status, status_date = sqlc.narg(status_date)
where id = @id and status = any(@allowed_from::text[]);

-- name: ChequeStatusTotals :many
select c.status, count(*)::int as n, coalesce(sum(p.amount), 0)::bigint as total
from cheques c join payments p on p.id = c.payment_id
group by c.status;

-- Panodaki çek özeti: yalnızca portföydeki çekler.
-- name: ChequeDueSummary :one
select
    count(*) filter (where c.due_date < @today::date)::int as overdue_n,
    coalesce(sum(p.amount) filter (where c.due_date < @today::date), 0)::bigint as overdue_total,
    count(*) filter (where c.due_date >= @today::date and c.due_date <= @soon::date)::int as soon_n,
    coalesce(sum(p.amount) filter (where c.due_date >= @today::date and c.due_date <= @soon::date), 0)::bigint as soon_total,
    count(*)::int as all_n,
    coalesce(sum(p.amount), 0)::bigint as all_total
from cheques c join payments p on p.id = c.payment_id
where c.status = 'PORTFOLIO';

-- Nakit akışı için: portföydeki çeklerin vade ve tutarı.
-- name: PortfolioChequeDues :many
select c.due_date, p.amount from cheques c join payments p on p.id = c.payment_id where c.status = 'PORTFOLIO' order by c.due_date;

-- Müşteri ödeme alışkanlığı: tahsilat sayısı/tutarı, çek payı, ortalama çek vadesi (gün), karşılıksız sayısı.
-- name: CustomerPaymentHabits :many
select cu.id as customer_id, cu.name as customer_name,
       count(p.id)::int as payment_n,
       coalesce(sum(p.amount), 0)::bigint as paid_total,
       coalesce(sum(p.amount) filter (where p.method in ('CHEQUE', 'NOTE')), 0)::bigint as cheque_total,
       coalesce(avg((c.due_date - p.paid_at)) filter (where c.id is not null), 0)::float8 as avg_term_days,
       count(c.id) filter (where c.status = 'BOUNCED')::int as bounced_n
from customers cu
join payments p on p.customer_id = cu.id and p.paid_at >= @since::date
left join cheques c on c.payment_id = p.id
group by cu.id, cu.name
order by paid_total desc;
