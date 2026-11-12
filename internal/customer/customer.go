// Package customer is the customer (cari kart) domain. It does not know Trendyol or the database.
package customer

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Kind is the customer type.
type Kind string

const (
	KindPerson  Kind = "PERSON"  // Şahıs (TCKN)
	KindCompany Kind = "COMPANY" // Şirket (VKN)
)

func (k Kind) Valid() bool { return k == KindPerson || k == KindCompany }

// Label is the Turkish display text.
func (k Kind) Label() string {
	if k == KindPerson {
		return "Şahıs"
	}
	return "Şirket"
}

// Customer is a cari kart. For persons Name holds "Ad Soyad".
type Customer struct {
	ID                uuid.UUID
	Kind              Kind
	TaxID             string // VKN (10) veya TCKN (11)
	Name              string
	TaxOffice         string
	Address           string
	District          string
	City              string
	PostalCode        string
	Email             string
	Phone             string
	Note              string
	Active            bool
	IsEInvoiceUser    bool
	EInvoiceAlias     string
	TaxpayerCheckedAt *time.Time
	OpeningBalance    int64 // kuruş
}

// FieldErrors maps a form field name to a Turkish message.
type FieldErrors map[string]string

var (
	emailRe    = regexp.MustCompile(`^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$`)
	phoneRe    = regexp.MustCompile(`^\+?[0-9]{7,15}$`)
	phoneNoise = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "")
)

// Normalize trims input and brings email/phone to the form the e-invoice API accepts.
// ALTIN KURAL 9: e-posta küçültme Türkçe locale ile yapılmaz (strings.ToLower invariant'tır).
func (c *Customer) Normalize() {
	for _, p := range []*string{&c.TaxID, &c.Name, &c.TaxOffice, &c.Address, &c.District, &c.City, &c.PostalCode, &c.Email, &c.Phone, &c.Note} {
		*p = strings.TrimSpace(*p)
	}
	c.Email = strings.ToLower(c.Email)
	c.Phone = phoneNoise.Replace(c.Phone)
}

// Validate checks the card against the e-invoice recipient rules (docs/trendyol-efaturam-api.md, recipientInfo).
// Call Normalize first.
func (c Customer) Validate() FieldErrors {
	errs := FieldErrors{}
	if !c.Kind.Valid() {
		errs["kind"] = "Müşteri türü seçilmeli."
	}
	switch {
	case !ValidTaxID(c.TaxID):
		errs["tax_id"] = "Geçerli bir VKN (10 hane) veya TCKN (11 hane) girin."
	case c.Kind == KindPerson && len(c.TaxID) != 11:
		errs["tax_id"] = "Şahıs için 11 haneli TCKN girin."
	case c.Kind == KindCompany && len(c.TaxID) != 10:
		errs["tax_id"] = "Şirket için 10 haneli VKN girin."
	}
	if n := utf8.RuneCountInString(c.Name); n < 2 || n > 127 {
		errs["name"] = "Ad/ünvan 2 ile 127 karakter arasında olmalı."
	}
	if utf8.RuneCountInString(c.City) < 2 {
		errs["city"] = "İl en az 2 karakter olmalı (fatura için zorunlu)."
	}
	if c.TaxOffice != "" && utf8.RuneCountInString(c.TaxOffice) < 2 {
		errs["tax_office"] = "Vergi dairesi en az 2 karakter olmalı."
	}
	if c.Address != "" && (utf8.RuneCountInString(c.Address) < 2 || utf8.RuneCountInString(c.Address) > 255) {
		errs["address"] = "Adres 2 ile 255 karakter arasında olmalı."
	}
	if c.PostalCode != "" && (utf8.RuneCountInString(c.PostalCode) != 5 || !allDigits(c.PostalCode)) {
		errs["postal_code"] = "Posta kodu tam 5 rakam olmalı."
	}
	if c.Email != "" && !emailRe.MatchString(c.Email) {
		errs["email"] = "E-posta adresi geçersiz."
	}
	if c.Phone != "" && !phoneRe.MatchString(c.Phone) {
		errs["phone"] = "Telefon 7–15 rakam olmalı (başında + olabilir)."
	}
	return errs
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// ValidTaxID reports whether s is a structurally valid VKN (10 digits) or TCKN (11 digits), including check digits.
func ValidTaxID(s string) bool {
	switch len(s) {
	case 10:
		return allDigits(s) && validVKN(s)
	case 11:
		return allDigits(s) && validTCKN(s)
	}
	return false
}

func validTCKN(s string) bool {
	if s[0] == '0' {
		return false
	}
	var d [11]int
	for i := range d {
		d[i] = int(s[i] - '0')
	}
	odd := d[0] + d[2] + d[4] + d[6] + d[8]
	even := d[1] + d[3] + d[5] + d[7]
	if ((odd*7-even)%10+10)%10 != d[9] {
		return false
	}
	sum := 0
	for _, v := range d[:10] {
		sum += v
	}
	return sum%10 == d[10]
}

func validVKN(s string) bool {
	sum := 0
	for i := range 9 {
		tmp := (int(s[i]-'0') + (9 - i)) % 10
		v := (tmp * (1 << (9 - i))) % 9
		if tmp != 0 && v == 0 {
			v = 9
		}
		sum += v
	}
	return (10-sum%10)%10 == int(s[9]-'0')
}
