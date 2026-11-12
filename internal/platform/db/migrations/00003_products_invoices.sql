-- +goose Up
create table products (
    id          uuid primary key default gen_random_uuid(),
    code        text not null default '',
    name        text not null,
    unit        text not null default 'C62',
    unit_price  bigint not null default 0 check (unit_price >= 0), -- kuruş, KDV hariç
    tax_percent integer not null default 20 check (tax_percent between 0 and 100),
    active      boolean not null default true,
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now()
);
create unique index products_code_key on products (code) where code <> '';

create table invoices (
    id          uuid primary key default gen_random_uuid(),
    status      text not null default 'DRAFT',
    customer_id uuid not null references customers (id),
    issue_date  date not null,
    due_date    date,
    notes       text not null default '',
    payable     bigint not null default 0, -- kuruş; taslakta her kayıtta yeniden hesaplanır
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now(),
    constraint invoices_due_after_issue check (due_date is null or due_date >= issue_date)
);
create index invoices_customer_idx on invoices (customer_id);
create index invoices_status_idx on invoices (status);

-- Kalemlere ürün adı/birim/fiyat/KDV KOPYALANIR: ürün sonradan değişse de fatura değişmez.
create table invoice_lines (
    id             uuid primary key default gen_random_uuid(),
    invoice_id     uuid not null references invoices (id) on delete cascade,
    position       integer not null,
    product_id     uuid references products (id) on delete set null,
    item_name      text not null,
    unit           text not null default 'C62',
    quantity_milli bigint not null check (quantity_milli > 0), -- miktar × 1000 (1,5 kg = 1500)
    unit_price     bigint not null check (unit_price >= 0),    -- kuruş
    discount       bigint not null default 0 check (discount >= 0), -- satır indirimi, kuruş
    tax_percent    integer not null check (tax_percent between 0 and 100)
);
create unique index invoice_lines_position_key on invoice_lines (invoice_id, position);

-- +goose Down
drop table invoice_lines;
drop table invoices;
drop table products;
