package auth

// Permission is one switchable right of a non-admin user. Admins have all of them.
// Görüntüleme (listeler, detay, PDF) her giriş yapmış kullanıcıya açıktır; aşağıdakiler değiştiren/hassas işlemlerdir.
type Permission string

const (
	PermCustomerWrite Permission = "customer.write"
	PermProductWrite  Permission = "product.write"
	PermInvoiceDraft  Permission = "invoice.draft"
	PermInvoiceIssue  Permission = "invoice.issue"
	PermInvoiceCancel Permission = "invoice.cancel"
	PermInvoiceDelete Permission = "invoice.delete"
	PermPaymentCreate Permission = "payment.create"
	PermPaymentDelete Permission = "payment.delete"
	PermReportView    Permission = "report.view"
	PermAuditView     Permission = "audit.view"
	PermDataImport    Permission = "data.import"
)

// PermInfo describes a permission for the admin screen.
type PermInfo struct {
	Key     Permission
	Label   string
	Group   string
	Default bool // yeni kullanıcıda önceden işaretli mi
}

// AllPermissions is the display order of the admin screen.
var AllPermissions = []PermInfo{
	{PermInvoiceDraft, "Fatura taslağı oluştur / düzenle", "Faturalar", true},
	{PermInvoiceIssue, "Faturayı kes (Trendyol'a gönder)", "Faturalar", false},
	{PermInvoiceCancel, "Faturayı iptal et", "Faturalar", false},
	{PermInvoiceDelete, "Taslak faturayı sil", "Faturalar", false},
	{PermPaymentCreate, "Tahsilat gir", "Tahsilat", true},
	{PermPaymentDelete, "Tahsilat sil", "Tahsilat", false},
	{PermCustomerWrite, "Müşteri ekle / düzenle", "Kayıtlar", true},
	{PermProductWrite, "Ürün ekle / düzenle", "Kayıtlar", true},
	{PermDataImport, "Excel'den müşteri içe aktar", "Kayıtlar", false},
	{PermReportView, "Pano, rapor, hatırlatma ve Excel dökümleri", "Raporlar", true},
	{PermAuditView, "İşlem geçmişini gör", "Raporlar", false},
}

// ValidPermission reports whether k is a known permission key.
func ValidPermission(k string) bool {
	for _, p := range AllPermissions {
		if string(p.Key) == k {
			return true
		}
	}
	return false
}

// PermissionList returns the user's explicit permissions in display order.
func (u User) PermissionList() []Permission {
	var out []Permission
	for _, p := range AllPermissions {
		if u.Perms[p.Key] {
			out = append(out, p.Key)
		}
	}
	return out
}

// Can reports whether the user may do p. Admins may do everything.
func (u User) Can(p Permission) bool {
	return u.IsAdmin || u.Perms[p]
}
