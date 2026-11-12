package invoice

import "strconv"

// ProviderStatus is the status code Trendyol E-Faturam reports for an invoice
// (docs/trendyol-efaturam-api.md, bölüm 9). Magic numbers never appear outside this file.
type ProviderStatus int

// Trendyol statü kodları.
const (
	StatusProcessing        ProviderStatus = 10  // İşleniyor
	StatusPreparingDocument ProviderStatus = 20  // Doküman hazırlanıyor
	StatusDocumentError     ProviderStatus = 29  // Doküman hatası
	StatusCreated           ProviderStatus = 30  // Oluşturuldu
	StatusSentToGIB         ProviderStatus = 40  // GİB'e gönderildi (e-Fatura)
	StatusAwaitingReply     ProviderStatus = 50  // Yanıt bekleniyor (ticari fatura)
	StatusRejecting         ProviderStatus = 100 // Reddediliyor
	StatusRejected          ProviderStatus = 105 // Reddedildi
	StatusApproving         ProviderStatus = 200 // Onaylanıyor
	StatusApproved          ProviderStatus = 205 // Onaylandı
	StatusCancelled         ProviderStatus = 305 // İptal edildi (e-Arşiv)
	StatusFailed            ProviderStatus = 405 // Hatalı
)

var statusNames = map[ProviderStatus]string{
	StatusProcessing:        "İşleniyor",
	StatusPreparingDocument: "Doküman hazırlanıyor",
	StatusDocumentError:     "Doküman hatası",
	StatusCreated:           "Oluşturuldu",
	StatusSentToGIB:         "GİB'e gönderildi",
	StatusAwaitingReply:     "Yanıt bekleniyor",
	StatusRejecting:         "Reddediliyor",
	StatusRejected:          "Reddedildi",
	StatusApproving:         "Onaylanıyor",
	StatusApproved:          "Onaylandı",
	StatusCancelled:         "İptal edildi",
	StatusFailed:            "Hatalı",
}

// Known reports whether s is a documented status code.
func (s ProviderStatus) Known() bool {
	_, ok := statusNames[s]
	return ok
}

// String returns the Turkish display name; unknown codes are shown with their number.
func (s ProviderStatus) String() string {
	if n, ok := statusNames[s]; ok {
		return n
	}
	return "Bilinmeyen durum (" + strconv.Itoa(int(s)) + ")"
}

// IsSuccess reports the final successful state (205).
func (s ProviderStatus) IsSuccess() bool { return s == StatusApproved }

// IsFailure reports the final error states: document error, rejected, failed.
func (s ProviderStatus) IsFailure() bool {
	return s == StatusDocumentError || s == StatusRejected || s == StatusFailed
}

// IsFinal reports whether the status will not change any more, so polling can stop.
// Cancelled (305) is final too. Unknown codes are NOT final: keep polling and let a human look.
func (s ProviderStatus) IsFinal() bool {
	return s.IsSuccess() || s.IsFailure() || s == StatusCancelled
}

// CountsTowardsLedger reports whether an invoice in this status is posted to the customer's
// account as a debt. Error and cancelled states are excluded (ROADMAP Faz 6); in-flight
// states count, because the invoice was handed to the provider.
func (s ProviderStatus) CountsTowardsLedger() bool {
	return !s.IsFailure() && s != StatusCancelled
}
