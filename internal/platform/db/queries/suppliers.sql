-- name: CreateSupplier :one
insert into suppliers (name, tax_id, phone, note, opening_balance) values ($1, $2, $3, $4, $5) returning id;

-- name: UpdateSupplier :execrows
update suppliers set name = $2, tax_id = $3, phone = $4, note = $5, opening_balance = $6, active = $7 where id = $1;

-- name: ListSuppliers :many
select s.id, s.name, s.tax_id, s.phone, s.note, s.opening_balance, s.active,
       (s.opening_balance
         + coalesce((select sum(e.amount) from supplier_entries e where e.supplier_id = s.id), 0)
         - coalesce((select sum(p.amount) from supplier_payments p where p.supplier_id = s.id), 0))::bigint as balance,
       coalesce((select sum(p.amount) from supplier_payments p where p.supplier_id = s.id and p.method = 'CHEQUE' and not p.cleared), 0)::bigint as pending_cheques
from suppliers s
where (@q::text = '' or s.name ilike '%' || @q || '%')
order by lower(s.name);

-- name: GetSupplier :one
select s.id, s.name, s.tax_id, s.phone, s.note, s.opening_balance, s.active,
       (s.opening_balance
         + coalesce((select sum(e.amount) from supplier_entries e where e.supplier_id = s.id), 0)
         - coalesce((select sum(p.amount) from supplier_payments p where p.supplier_id = s.id), 0))::bigint as balance,
       coalesce((select sum(p.amount) from supplier_payments p where p.supplier_id = s.id and p.method = 'CHEQUE' and not p.cleared), 0)::bigint as pending_cheques
from suppliers s where s.id = $1;

-- name: CreateSupplierEntry :one
insert into supplier_entries (supplier_id, entry_date, amount, doc_no, description, due_date) values ($1, $2, $3, $4, $5, $6) returning id;

-- name: DeleteSupplierEntry :execrows
delete from supplier_entries where id = $1;

-- name: ListSupplierEntries :many
select e.id, e.supplier_id, e.entry_date, e.amount, e.doc_no, e.description, e.due_date,
       coalesce((select sum(p.amount) from supplier_payments p where p.entry_id = e.id), 0)::bigint as paid
from supplier_entries e where e.supplier_id = $1 order by e.entry_date, e.created_at;

-- name: CreateSupplierPayment :one
insert into supplier_payments (supplier_id, paid_at, amount, method, cheque_due, cheque_no, bank, from_cheque_id, entry_id, note)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) returning id;

-- name: DeleteSupplierPayment :one
delete from supplier_payments where id = $1 returning from_cheque_id;

-- name: ListSupplierPayments :many
select p.id, p.supplier_id, p.paid_at, p.amount, p.method, p.cheque_due, p.cheque_no, p.bank, p.cleared, p.from_cheque_id, p.entry_id, p.note
from supplier_payments p where p.supplier_id = $1 order by p.paid_at, p.created_at;

-- name: SetSupplierChequeCleared :execrows
update supplier_payments set cleared = $2 where id = $1 and method = 'CHEQUE';

-- Nakit akışı: henüz hesabımızdan çıkmamış kendi çeklerimiz.
-- name: PendingOwnChequeDues :many
select p.cheque_due::date as due_date, p.amount from supplier_payments p where p.method = 'CHEQUE' and not p.cleared order by p.cheque_due;

-- Verdiğimiz çekler listesi (vade sırasıyla).
-- name: ListOwnCheques :many
select p.id, p.supplier_id, s.name as supplier_name, p.paid_at, p.amount, p.cheque_due, p.cheque_no, p.bank, p.cleared
from supplier_payments p join suppliers s on s.id = p.supplier_id
where p.method = 'CHEQUE'
order by p.cleared, p.cheque_due;

-- name: SupplierDebtTotal :one
select coalesce(sum(
        s.opening_balance
        + coalesce((select sum(e.amount) from supplier_entries e where e.supplier_id = s.id), 0)
        - coalesce((select sum(p.amount) from supplier_payments p where p.supplier_id = s.id), 0)
    ), 0)::bigint as total
from suppliers s where s.active;
