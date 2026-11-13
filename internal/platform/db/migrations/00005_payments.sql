-- +goose Up
create table payments (
    id          uuid primary key default gen_random_uuid(),
    customer_id uuid not null references customers (id),
    paid_at     date not null,
    amount      bigint not null check (amount > 0), -- kuruş
    method      text not null check (method in ('CASH', 'TRANSFER', 'CARD', 'CHEQUE', 'NOTE', 'OTHER')),
    note        text not null default '',
    invoice_id  uuid references invoices (id) on delete set null, -- isteğe bağlı: bu faturaya mahsup
    created_at  timestamptz not null default now()
);
create index payments_customer_idx on payments (customer_id, paid_at);
create index payments_paid_at_idx on payments (paid_at);

-- +goose Down
drop table payments;
