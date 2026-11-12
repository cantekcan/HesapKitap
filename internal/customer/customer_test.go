package customer_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"hesapkitap/internal/customer"
)

func TestValidTaxID(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"10000000146", true},  // bilinen geçerli TCKN
		{"10000000147", false}, // son hane hatalı
		{"00000000000", false},
		{"1000000014", false}, // 10 hane ama VKN sağlaması tutmaz
		{"1234567891", false},
		{"abcdefghij", false},
		{"", false},
		{"123456789012", false},
	} {
		assert.Equal(t, tc.want, customer.ValidTaxID(tc.id), tc.id)
	}
}

func TestValidVKN(t *testing.T) {
	// Sağlama hanesini algoritmadan üretip doğruluyoruz (gerçek bir VKN kullanmamak için).
	base := "123456789"
	var found string
	for d := '0'; d <= '9'; d++ {
		if customer.ValidTaxID(base + string(d)) {
			found = base + string(d)
		}
	}
	assert.NotEmpty(t, found, "9 haneli gövde için tam bir sağlama hanesi olmalı")
	for d := '0'; d <= '9'; d++ {
		if c := base + string(d); c != found {
			assert.False(t, customer.ValidTaxID(c), c)
		}
	}
}

func valid() customer.Customer {
	return customer.Customer{Kind: customer.KindPerson, TaxID: "10000000146", Name: "Ali Veli", City: "İstanbul", District: "Kadıköy"}
}

func TestValidate(t *testing.T) {
	c := valid()
	c.Normalize()
	assert.Empty(t, c.Validate())

	cases := map[string]func(*customer.Customer){
		"tax_id":      func(c *customer.Customer) { c.TaxID = "123" },
		"name":        func(c *customer.Customer) { c.Name = "A" },
		"city":        func(c *customer.Customer) { c.City = "" },
		"district":    func(c *customer.Customer) { c.District = "" },
		"postal_code": func(c *customer.Customer) { c.PostalCode = "123" },
		"email":       func(c *customer.Customer) { c.Email = "yanlis" },
		"phone":       func(c *customer.Customer) { c.Phone = "12" },
		"address":     func(c *customer.Customer) { c.Address = "x" },
		"kind":        func(c *customer.Customer) { c.Kind = "" },
	}
	for field, mutate := range cases {
		c := valid()
		mutate(&c)
		c.Normalize()
		assert.Contains(t, c.Validate(), field, field)
	}

	// Tür ile VKN/TCKN uzunluğu tutarlı olmalı.
	c = valid()
	c.Kind = customer.KindCompany
	assert.Contains(t, c.Validate(), "tax_id")
}

func TestNormalize(t *testing.T) {
	c := valid()
	c.Email = "  Ali.Veli@ORNEK.com "
	c.Phone = "+90 (532) 111-22 33"
	c.Name = "  Ali Veli  "
	c.Normalize()
	assert.Equal(t, "ali.veli@ornek.com", c.Email)
	assert.Equal(t, "+905321112233", c.Phone)
	assert.Equal(t, "Ali Veli", c.Name)
	assert.Empty(t, c.Validate())
}

func TestEmailLowercaseIsNotTurkishLocale(t *testing.T) {
	// ALTIN KURAL 9: "I" küçültülürken "ı" değil "i" olmalı.
	c := valid()
	c.Email = "INFO@FIRMA.COM"
	c.Normalize()
	assert.Equal(t, "info@firma.com", c.Email)
	assert.False(t, strings.Contains(c.Email, "ı"))
}
