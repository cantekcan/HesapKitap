-- +goose Up

-- Müşteriden alınan çek/senet: tahsilat kaydına bağlıdır, vadesi ve durumu burada izlenir.
create table cheques (
    id          uuid primary key default gen_random_uuid(),
    payment_id  uuid not null unique references payments (id) on delete cascade,
    number      text not null default '',
    bank        text not null default '',
    drawer      text not null default '', -- keşideci (çeki yazan)
    due_date    date not null,
    status      text not null default 'PORTFOLIO' check (status in ('PORTFOLIO', 'COLLECTED', 'BOUNCED', 'ENDORSED')),
    status_date date,
    created_at  timestamptz not null default now()
);
create index cheques_status_due_idx on cheques (status, due_date);

-- Tedarikçiler (toptancılar): bizim borçlarımız.
create table suppliers (
    id              uuid primary key default gen_random_uuid(),
    name            text not null,
    tax_id          text not null default '',
    phone           text not null default '',
    note            text not null default '',
    opening_balance bigint not null default 0, -- kuruş; + bizim onlara borcumuz
    active          boolean not null default true,
    created_at      timestamptz not null default now()
);
create unique index suppliers_name_idx on suppliers (lower(name));

-- Mal alımı / borç kaydı (elle girilir).
create table supplier_entries (
    id          uuid primary key default gen_random_uuid(),
    supplier_id uuid not null references suppliers (id),
    entry_date  date not null,
    amount      bigint not null check (amount > 0),
    doc_no      text not null default '', -- fatura / irsaliye no
    description text not null default '',
    due_date    date,
    created_at  timestamptz not null default now()
);
create index supplier_entries_supplier_idx on supplier_entries (supplier_id, entry_date);

-- Tedarikçiye yapılan ödeme. CHEQUE: kendi çekimiz (vadeli), ENDORSED: müşteriden aldığımız çeki ciro ettik.
create table supplier_payments (
    id             uuid primary key default gen_random_uuid(),
    supplier_id    uuid not null references suppliers (id),
    paid_at        date not null,
    amount         bigint not null check (amount > 0),
    method         text not null check (method in ('CASH', 'TRANSFER', 'CARD', 'CHEQUE', 'ENDORSED', 'NOTE', 'OTHER')),
    cheque_due     date,
    cheque_no      text not null default '',
    bank           text not null default '',
    cleared        boolean not null default false, -- kendi çekimiz hesabımızdan çıktı mı
    from_cheque_id uuid unique references cheques (id),
    entry_id       uuid references supplier_entries (id) on delete set null, -- hangi alıma karşılık
    note           text not null default '',
    created_at     timestamptz not null default now(),
    check (method <> 'CHEQUE' or cheque_due is not null),
    check (method <> 'ENDORSED' or from_cheque_id is not null)
);
create index supplier_payments_supplier_idx on supplier_payments (supplier_id, paid_at);

-- Fotoğraf / belge ekleri (çek fotoğrafı, teslim fişi...). Dosyalar veritabanında durur, yedekle birlikte gider.
create table attachments (
    id           uuid primary key default gen_random_uuid(),
    owner_type   text not null check (owner_type in ('payment', 'cheque', 'supplier_entry', 'supplier_payment')),
    owner_id     uuid not null,
    filename     text not null,
    content_type text not null,
    size         integer not null,
    data         bytea not null,
    uploaded_by  text not null default '',
    created_at   timestamptz not null default now()
);
create index attachments_owner_idx on attachments (owner_type, owner_id);

-- Kaydı silinen sahibin ekleri de silinir (sahipsiz dosya kalmaz).
-- +goose StatementBegin
create function attachments_cleanup() returns trigger language plpgsql as $$
begin
    delete from attachments where owner_type = tg_argv[0] and owner_id = old.id;
    return old;
end;
$$;
-- +goose StatementEnd
create trigger payments_attachments after delete on payments for each row execute function attachments_cleanup('payment');
create trigger cheques_attachments after delete on cheques for each row execute function attachments_cleanup('cheque');
create trigger supplier_entries_attachments after delete on supplier_entries for each row execute function attachments_cleanup('supplier_entry');
create trigger supplier_payments_attachments after delete on supplier_payments for each row execute function attachments_cleanup('supplier_payment');

-- +goose Down
drop trigger supplier_payments_attachments on supplier_payments;
drop trigger supplier_entries_attachments on supplier_entries;
drop trigger cheques_attachments on cheques;
drop trigger payments_attachments on payments;
drop function attachments_cleanup();
drop table attachments;
drop table supplier_payments;
drop table supplier_entries;
drop table suppliers;
drop table cheques;
