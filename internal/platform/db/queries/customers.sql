-- name: GetCustomer :one
select * from customers where id = $1;

-- name: CreateCustomer :one
insert into customers (kind, tax_id, name, tax_office, address, district, city, postal_code, email, phone, note, active, opening_balance)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
returning *;

-- name: UpdateCustomer :execrows
update customers set
    kind = $2, tax_id = $3, name = $4, tax_office = $5, address = $6, district = $7, city = $8,
    postal_code = $9, email = $10, phone = $11, note = $12, active = $13, opening_balance = $14,
    updated_at = now()
where id = $1;

-- name: SaveCustomerTaxpayer :execrows
update customers
set is_einvoice_user = $2, einvoice_alias = $3, taxpayer_checked_at = $4, updated_at = now()
where id = $1;

-- name: CountCustomers :one
select count(*) from customers
where (@q::text = '' or name ilike '%' || @q || '%' or tax_id like @q || '%')
  and (@status::text = '' or (@status = 'active' and active) or (@status = 'passive' and not active));

-- name: ListCustomers :many
select * from customers
where (@q::text = '' or name ilike '%' || @q || '%' or tax_id like @q || '%')
  and (@status::text = '' or (@status = 'active' and active) or (@status = 'passive' and not active))
order by lower(name), id
limit @lim offset @off;
