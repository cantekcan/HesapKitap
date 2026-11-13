-- +goose Up
-- İşlem geçmişi: kim, ne zaman, neyi yaptı. Yalnızca eklenir; uygulama silmez/değiştirmez.
create table audit_log (
    id          bigserial primary key,
    at          timestamptz not null default now(),
    actor       text not null,            -- kullanıcı e-postası
    action      text not null,            -- ör. invoice.issue
    entity      text not null default '', -- ör. invoice, customer, payment
    entity_id   text not null default '',
    summary     text not null default ''
);
create index audit_log_at_idx on audit_log (at desc);
create index audit_log_entity_idx on audit_log (entity, entity_id);

-- +goose Down
drop table audit_log;
