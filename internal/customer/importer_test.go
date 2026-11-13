package customer

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memStore struct{ byTax map[string]Customer }

func (m *memStore) Get(context.Context, uuid.UUID) (Customer, error) { return Customer{}, ErrNotFound }
func (m *memStore) List(context.Context, ListFilter) ([]Customer, int, error) {
	return nil, 0, nil
}
func (m *memStore) Create(_ context.Context, c Customer) (Customer, error) {
	if _, dup := m.byTax[c.TaxID]; dup {
		return Customer{}, ErrDuplicateTaxID
	}
	c.ID = uuid.New()
	m.byTax[c.TaxID] = c
	return c, nil
}
func (m *memStore) Update(context.Context, Customer) error { return nil }
func (m *memStore) SaveTaxpayer(context.Context, uuid.UUID, bool, string, time.Time) error {
	return nil
}

func TestImport(t *testing.T) {
	st := &memStore{byTax: map[string]Customer{}}
	s := &Service{Store: st}
	rows := [][]string{
		{"Ünvan", "Tür", "VKN/TCKN", "Vergi dairesi", "İl", "İlçe", "Açılış bakiyesi", "Bilinmeyen"},
		{"Ali Veli", "", "10000000146", "", "İstanbul", "Kadıköy", "1.250,50", "x"},
		{"Boş Satır Sonrası", "Şirket", "1234567890", "Kadıköy", "İstanbul", "Kadıköy", "", ""},
		{"", "", "", "", "", "", "", ""},
		{"Aynı TCKN", "", "10000000146", "", "İstanbul", "Kadıköy", "", ""},
		{"İlçesiz", "", "10000000146 ", "", "İstanbul", "", "", ""},
		{"Hatalı Bakiye", "", "98765432109", "", "İstanbul", "Kadıköy", "abc", ""},
	}
	res, err := s.Import(context.Background(), rows)
	require.NoError(t, err)
	require.Len(t, res, 5)
	assert.True(t, res[0].Created)
	assert.Equal(t, int64(125050), st.byTax["10000000146"].OpeningBalance)
	assert.Equal(t, KindPerson, st.byTax["10000000146"].Kind) // 11 hane → şahıs
	assert.True(t, res[1].Created)
	assert.Equal(t, KindCompany, st.byTax["1234567890"].Kind)
	assert.False(t, res[2].Created)
	assert.Contains(t, res[2].Message, "kayıtlı başka bir müşteri")
	assert.Equal(t, 5, res[2].Row)
	assert.False(t, res[3].Created)
	assert.Contains(t, res[3].Message, "İlçe")
	assert.Contains(t, res[4].Message, "Açılış bakiyesi okunamadı")
}

func TestImportNeedsRequiredColumns(t *testing.T) {
	s := &Service{Store: &memStore{byTax: map[string]Customer{}}}
	_, err := s.Import(context.Background(), [][]string{{"Ad", "Şehir"}})
	require.Error(t, err)
	_, err = s.Import(context.Background(), nil)
	require.Error(t, err)
}

func TestNormalizeTaxIDRestoresLostZero(t *testing.T) {
	assert.Equal(t, "0123456789", normalizeTaxID("123456789"))
	assert.Equal(t, "1234567890", normalizeTaxID(" 1234567890.0 "))
}
