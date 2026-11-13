-- +goose Up
-- Roller: yönetici (her şeye yetkili, kullanıcıları yönetir) ve yetkileri tek tek açılıp kapatılan kullanıcı.
alter table users
    add column is_admin    boolean not null default false,
    add column active      boolean not null default true,
    add column permissions text[] not null default '{}';
-- Bu migration'dan önce var olan kullanıcılar sistemi kuranlardır: yönetici olurlar.
update users set is_admin = true;

-- +goose Down
alter table users drop column permissions, drop column active, drop column is_admin;
