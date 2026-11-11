-- +goose Up
create table users (
    id              uuid primary key default gen_random_uuid(),
    email           text not null,
    password_hash   text not null,
    failed_attempts integer not null default 0,
    locked_until    timestamptz,
    created_at      timestamptz not null default now(),
    constraint users_email_lowercase check (email = lower(email))
);
create unique index users_email_key on users (email);

-- alexedwards/scs pgxstore tablosu
create table sessions (
    token  text primary key,
    data   bytea not null,
    expiry timestamptz not null
);
create index sessions_expiry_idx on sessions (expiry);

-- +goose Down
drop table sessions;
drop table users;
