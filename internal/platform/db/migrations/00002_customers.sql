-- +goose Up
create table customers (
    id                  uuid primary key default gen_random_uuid(),
    kind                text not null check (kind in ('PERSON', 'COMPANY')),
    tax_id              text not null check (tax_id ~ '^[0-9]{10}$' or tax_id ~ '^[0-9]{11}$'),
    name                text not null,
    tax_office          text not null default '',
    address             text not null default '',
    district            text not null default '',
    city                text not null default '',
    postal_code         text not null default '',
    email               text not null default '',
    phone               text not null default '',
    note                text not null default '',
    active              boolean not null default true,
    is_einvoice_user    boolean not null default false,
    einvoice_alias      text not null default '',
    taxpayer_checked_at timestamptz,
    opening_balance     bigint not null default 0, -- kuruş; + müşteri bize borçlu
    created_at          timestamptz not null default now(),
    updated_at          timestamptz not null default now()
);
create unique index customers_tax_id_key on customers (tax_id);

-- +goose Down
drop table customers;
