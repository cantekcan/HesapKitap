package httpserver

import (
	"net/http"
	"strconv"

	"github.com/xuri/excelize/v2"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/platform/http/views"
)

const maxImportBytes = 5 << 20

func (s *server) customerImportForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, views.CustomerImportPage(s.pageData(r, "customers", "Müşteri içe aktar"), views.ImportPage{}))
}

func (s *server) customerImportTemplate(w http.ResponseWriter, _ *http.Request) {
	b := newBook()
	b.open("Müşteriler", true, 34, 10, 16, 20, 14, 14, 36, 12, 26, 16, 16)
	b.headers(customer.ImportColumns...)
	for i, v := range []any{"Örnek Ticaret A.Ş.", "Şirket", "1234567890", "Kadıköy", "İstanbul", "Kadıköy", "Örnek Mah. 1. Sok. No:2", "34710", "info@ornek.com", "05321112233", "1250,50"} {
		b.cell(i+1, v, 0)
	}
	b.send(w, "musteri-sablonu.xlsx")
}

func (s *server) customerImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
	if err := r.ParseMultipartForm(maxImportBytes); err != nil { //nolint:gosec // gövde yukarıda MaxBytesReader ile sınırlandı
		render(w, r, http.StatusRequestEntityTooLarge, views.CustomerImportPage(s.pageData(r, "customers", "Müşteri içe aktar"),
			views.ImportPage{Error: "Dosya okunamadı veya 5 MB'tan büyük."}))
		return
	}
	fail := func(msg string) {
		render(w, r, http.StatusUnprocessableEntity, views.CustomerImportPage(s.pageData(r, "customers", "Müşteri içe aktar"), views.ImportPage{Error: msg}))
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		fail("Bir .xlsx dosyası seçin.")
		return
	}
	defer func() { _ = file.Close() }()
	x, err := excelize.OpenReader(file)
	if err != nil {
		fail("Dosya geçerli bir Excel (.xlsx) dosyası değil.")
		return
	}
	defer func() { _ = x.Close() }()
	rows, err := x.GetRows(x.GetSheetName(0))
	if err != nil {
		fail("Excel sayfası okunamadı.")
		return
	}
	results, err := s.Customers.Import(r.Context(), rows)
	if err != nil && len(results) == 0 {
		fail(err.Error())
		return
	}
	if err != nil {
		s.serverError(w, r, "müşteri içe aktarma yarıda kaldı", err)
		return
	}
	created := 0
	for _, res := range results {
		if res.Created {
			created++
		}
	}
	s.record(r, "customer.import", "", "", strconv.Itoa(created)+" müşteri Excel'den eklendi")
	render(w, r, http.StatusOK, views.CustomerImportPage(s.pageData(r, "customers", "Müşteri içe aktar"), views.ImportPage{Results: results, Done: true}))
}
