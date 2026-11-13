// Package audit is the append-only action history (işlem geçmişi).
package audit

import (
	"context"
	"time"
)

// Entry is one recorded action.
type Entry struct {
	ID       int64
	At       time.Time
	Actor    string
	Action   string // ör. "invoice.issue"
	Entity   string // ör. "invoice"
	EntityID string
	Summary  string
}

// Store is the persistence port, implemented in internal/platform/db.
type Store interface {
	Add(ctx context.Context, e Entry) error
	List(ctx context.Context, query string, limit, offset int) ([]Entry, int, error)
}

// Labels are the Turkish names of the actions shown on the history page.
var Labels = map[string]string{ //nolint:gosec // G101 yanlış alarm: "user.password" bir işlem adı, şifre değil
	"login":           "Giriş yapıldı",
	"customer.create": "Müşteri eklendi",
	"customer.update": "Müşteri güncellendi",
	"customer.import": "Müşteriler içe aktarıldı",
	"product.create":  "Ürün eklendi",
	"product.update":  "Ürün güncellendi",
	"invoice.save":    "Fatura taslağı kaydedildi",
	"invoice.delete":  "Fatura taslağı silindi",
	"invoice.issue":   "Fatura kesildi",
	"invoice.cancel":  "Fatura iptal edildi",
	"payment.create":  "Tahsilat girildi",
	"payment.delete":  "Tahsilat silindi",
	"user.create":     "Kullanıcı eklendi",
	"user.update":     "Kullanıcı yetkileri güncellendi",
	"user.password":   "Kullanıcı şifresi değiştirildi",
}

// Label returns the Turkish text of an action (the raw code if unknown).
func Label(action string) string {
	if l, ok := Labels[action]; ok {
		return l
	}
	return action
}
