-- name: GetProduct :one
select * from products where id = $1;

-- name: CreateProduct :one
insert into products (code, name, unit, unit_price, tax_percent, active)
values ($1, $2, $3, $4, $5, $6)
returning *;

-- name: UpdateProduct :execrows
update products set code = $2, name = $3, unit = $4, unit_price = $5, tax_percent = $6, active = $7, updated_at = now()
where id = $1;

-- name: CountProducts :one
select count(*) from products
where (@q::text = '' or name ilike '%' || @q || '%' or code ilike @q || '%')
  and (@status::text = '' or (@status = 'active' and active) or (@status = 'passive' and not active));

-- name: ListProducts :many
select * from products
where (@q::text = '' or name ilike '%' || @q || '%' or code ilike @q || '%')
  and (@status::text = '' or (@status = 'active' and active) or (@status = 'passive' and not active))
order by lower(name), id
limit @lim offset @off;

-- name: ListActiveProducts :many
select * from products where active order by lower(name), id;
