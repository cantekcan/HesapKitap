package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/payment"
	"hesapkitap/internal/platform/db"
	"hesapkitap/internal/platform/db/dbgen"
	"hesapkitap/internal/platform/db/dbtest"
)

func TestLedgerAndPaymentStores(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := context.Background()
	q := dbgen.New(pool)

	c, err := db.NewCustomerStore(q).Create(ctx, customer.Customer{
		Kind: customer.KindCompany, TaxID: "1234567890", Name: "Deneme AŞ", City: "İstanbul", District: "Kadıköy", Active: true, OpeningBalance: 1000,
	})
	require.NoError(t, err)

	// 3 fatura: gönderilmiş, iptal (305) ve taslak; yalnızca ilki cariye girer.
	insert := func(status string, ps *int, payable int64) {
		_, err := pool.Exec(ctx, `insert into invoices (customer_id, status, issue_date, due_date, payable, provider_status, invoice_number)
			values ($1, $2, '2026-01-10', '2026-02-10', $3, $4, 'ABC2026000000001')`, c.ID, status, payable, ps)
		require.NoError(t, err)
	}
	ok, cancelled := 205, 305
	insert("ISSUED", &ok, 10000)
	insert("CANCELLED", &cancelled, 5000)
	insert("DRAFT", nil, 7000)

	ps := db.NewPaymentStore(q)
	svc := &payment.Service{Store: ps, Now: func() time.Time { return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) }}
	_, verrs, err := svc.Create(ctx, payment.Payment{CustomerID: c.ID, Date: time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC), Amount: 4000, Method: payment.MethodCash})
	require.NoError(t, err)
	require.Empty(t, verrs)

	ls := db.NewLedgerStore(q)
	d, err := ls.Customer(ctx, c.ID)
	require.NoError(t, err)
	require.Len(t, d.Invoices, 1)
	require.Equal(t, int64(7000), d.Balance()) // 1000 açılış + 10000 fatura − 4000 tahsilat

	pos := d.Position(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	require.Equal(t, int64(7000), pos.Overdue) // vade 10 Şubat geçti

	all, err := ls.All(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, int64(7000), all[0].Balance())

	items, total, err := ps.List(ctx, payment.ListFilter{Query: "deneme", Limit: 10})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, "Deneme AŞ", items[0].CustomerName)
	sum, err := ps.Total(ctx, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Equal(t, int64(4000), sum)

	require.NoError(t, ps.Delete(ctx, items[0].ID))
	require.ErrorIs(t, ps.Delete(ctx, items[0].ID), payment.ErrNotFound)
}
