-- +goose Up
-- Faturayı kesme: ETTN gönderimden ÖNCE üretilip yazılır (idempotency), alıcı bilgisi o anda dondurulur.
alter table invoices
    add column kind            text check (kind in ('EARCHIVE', 'EINVOICE')),
    add column scenario        text not null default '',   -- EARSIVFATURA | TEMELFATURA | TICARIFATURA
    add column ettn            uuid,
    add column provider_status integer,
    add column invoice_number  text not null default '',
    add column target_alias    text not null default '',
    add column recipient       jsonb,
    add column last_error      text not null default '',
    add column sent_at         timestamptz;
create unique index invoices_ettn_key on invoices (ettn) where ettn is not null;
create index invoices_pending_idx on invoices (status) where status in ('SENDING', 'ISSUED');

-- +goose Down
drop index invoices_pending_idx;
drop index invoices_ettn_key;
alter table invoices
    drop column sent_at, drop column last_error, drop column recipient, drop column target_alias,
    drop column invoice_number, drop column provider_status, drop column ettn, drop column scenario, drop column kind;
