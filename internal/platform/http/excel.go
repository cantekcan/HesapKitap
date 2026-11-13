package httpserver

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/ledger"
	"hesapkitap/internal/payment"
	"hesapkitap/internal/report"
)

// Excel dışa aktarma (excelize). Tutarlar yalnızca gösterim için kuruş/100 olarak yazılır; hesap yapılmaz.

const xlsxType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

type sheetBook struct {
	f      *excelize.File
	header int
	money  int
	date   int
	bold   int
	title  int
	sheet  string
	row    int
}

func newBook() *sheetBook {
	f := excelize.NewFile()
	b := &sheetBook{f: f}
	b.header, _ = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"4F46E5"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", Horizontal: "left"},
	})
	b.money, _ = f.NewStyle(&excelize.Style{NumFmt: 4}) // #,##0.00
	b.date, _ = f.NewStyle(&excelize.Style{NumFmt: 14})
	b.bold, _ = f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, NumFmt: 4})
	b.title, _ = f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}})
	return b
}

// open starts a sheet (the first call renames the default sheet).
func (b *sheetBook) open(name string, first bool, widths ...float64) {
	if first {
		_ = b.f.SetSheetName(b.f.GetSheetName(0), name)
	} else {
		_, _ = b.f.NewSheet(name)
	}
	b.sheet, b.row = name, 1
	for i, w := range widths {
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = b.f.SetColWidth(name, col, col, w)
	}
}

func (b *sheetBook) cell(col int, v any, style int) {
	ref, _ := excelize.CoordinatesToCellName(col, b.row)
	_ = b.f.SetCellValue(b.sheet, ref, v)
	if style != 0 {
		_ = b.f.SetCellStyle(b.sheet, ref, ref, style)
	}
}

func (b *sheetBook) heading(text string) {
	b.cell(1, text, b.title)
	b.row += 2
}

func (b *sheetBook) headers(names ...string) {
	for i, n := range names {
		b.cell(i+1, n, b.header)
	}
	b.row++
}

func kurus(v int64) float64 { return float64(v) / 100 }

func (b *sheetBook) send(w http.ResponseWriter, filename string) {
	w.Header().Set("Content-Type", xlsxType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	_ = b.f.Write(w)
}

func fileSafe(s string) string {
	r := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "\"", "", "*", "", "?", "", "<", "", ">", "", "|", "")
	return r.Replace(s)
}

func (s *server) salesXLSX(w http.ResponseWriter, r *http.Request) {
	rep, from, to, ok := s.salesReport(w, r)
	if !ok {
		return
	}
	b := newBook()
	b.open("KDV özeti", true, 14, 18, 18)
	b.heading("Satış raporu " + from.Format("02.01.2006") + " – " + to.Format("02.01.2006"))
	b.headers("KDV oranı (%)", "Matrah", "KDV")
	for _, rr := range rep.Rates {
		b.cell(1, rr.Percent, 0)
		b.cell(2, kurus(rr.Taxable), b.money)
		b.cell(3, kurus(rr.Tax), b.money)
		b.row++
	}
	b.cell(1, "Toplam", b.bold)
	b.cell(2, kurus(rep.Taxable), b.bold)
	b.cell(3, kurus(rep.Tax), b.bold)

	b.open("Aylık", false, 18, 10, 18, 18, 18)
	b.headers("Ay", "Adet", "Matrah", "KDV", "Toplam")
	for _, m := range rep.Months {
		b.cell(1, monthLabel(m.Month), 0)
		b.cell(2, m.Count, 0)
		b.cell(3, kurus(m.Taxable), b.money)
		b.cell(4, kurus(m.Tax), b.money)
		b.cell(5, kurus(m.Total), b.money)
		b.row++
	}

	b.open("Faturalar", false, 12, 20, 36, 16, 16, 16)
	b.headers("Tarih", "Fatura no", "Müşteri", "Matrah", "KDV", "Toplam")
	for _, in := range rep.Invoices {
		b.cell(1, in.IssueDate, b.date)
		b.cell(2, in.Number, 0)
		b.cell(3, in.Customer, 0)
		b.cell(4, kurus(in.Taxable), b.money)
		b.cell(5, kurus(in.Tax), b.money)
		b.cell(6, kurus(in.Total), b.money)
		b.row++
	}
	b.send(w, "satis-raporu-"+formatDate(from)+"_"+formatDate(to)+".xlsx")
}

var trMonths = [...]string{"Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran", "Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık"}

func monthLabel(t time.Time) string { return trMonths[t.Month()-1] + " " + t.Format("2006") }

func (s *server) agingXLSX(w http.ResponseWriter, r *http.Request) {
	all, err := s.Ledger.All(r.Context())
	if err != nil {
		s.serverError(w, r, "yaşlandırma raporu okunamadı", err)
		return
	}
	a := report.BuildAging(all, today())
	b := newBook()
	b.open("Alacak yaşlandırma", true, 36, 16, 14, 14, 14, 14, 16, 16)
	b.headers(append(append([]string{"Müşteri"}, report.BucketLabels[:]...), "Toplam")...)
	for _, row := range a.Rows {
		b.cell(1, row.Name, 0)
		for i, v := range row.Buckets {
			b.cell(i+2, kurus(v), b.money)
		}
		b.cell(8, kurus(row.Total), b.bold)
		b.row++
	}
	b.cell(1, "Toplam", b.bold)
	for i, v := range a.Buckets {
		b.cell(i+2, kurus(v), b.bold)
	}
	b.cell(8, kurus(a.Total), b.bold)
	b.send(w, "alacak-yaslandirma-"+formatDate(today())+".xlsx")
}

func (s *server) paymentsXLSX(w http.ResponseWriter, r *http.Request) {
	from, to := parseDate(r.URL.Query().Get("from")), parseDate(r.URL.Query().Get("to"))
	items, _, err := s.Payments.List(r.Context(), payment.ListFilter{Query: strings.TrimSpace(r.URL.Query().Get("q")), From: from, To: to, Limit: 100000})
	if err != nil {
		s.serverError(w, r, "tahsilatlar okunamadı", err)
		return
	}
	b := newBook()
	b.open("Tahsilatlar", true, 12, 36, 16, 40, 16)
	b.headers("Tarih", "Müşteri", "Yöntem", "Açıklama", "Tutar")
	var sum int64
	for _, p := range items {
		b.cell(1, p.Date, b.date)
		b.cell(2, p.CustomerName, 0)
		b.cell(3, p.Method.Label(), 0)
		b.cell(4, p.Note, 0)
		b.cell(5, kurus(p.Amount), b.money)
		sum += p.Amount
		b.row++
	}
	b.cell(4, "Toplam", b.bold)
	b.cell(5, kurus(sum), b.bold)
	b.send(w, "tahsilatlar-"+formatDate(today())+".xlsx")
}

func (s *server) customersXLSX(w http.ResponseWriter, r *http.Request) {
	cs, _, err := s.Customers.List(r.Context(), customer.ListFilter{Limit: 100000})
	if err != nil {
		s.serverError(w, r, "müşteriler okunamadı", err)
		return
	}
	all, err := s.Ledger.All(r.Context())
	if err != nil {
		s.serverError(w, r, "bakiyeler okunamadı", err)
		return
	}
	pos := make(map[uuid.UUID]ledger.Position, len(all))
	for _, d := range all {
		pos[d.CustomerID] = d.Position(today())
	}
	b := newBook()
	b.open("Müşteriler", true, 36, 14, 14, 20, 16, 16, 30, 16, 16, 16)
	b.headers("Ünvan", "Tür", "VKN/TCKN", "Vergi dairesi", "İl", "İlçe", "E-posta", "Telefon", "Bakiye", "Vadesi geçmiş")
	for _, c := range cs {
		p := pos[c.ID]
		kind := "Şirket"
		if c.Kind == customer.KindPerson {
			kind = "Şahıs"
		}
		b.cell(1, c.Name, 0)
		b.cell(2, kind, 0)
		b.cell(3, c.TaxID, 0) // metin: baştaki sıfırlar kaybolmasın
		b.cell(4, c.TaxOffice, 0)
		b.cell(5, c.City, 0)
		b.cell(6, c.District, 0)
		b.cell(7, c.Email, 0)
		b.cell(8, c.Phone, 0)
		b.cell(9, kurus(p.Balance), b.money)
		b.cell(10, kurus(p.Overdue), b.money)
		b.row++
	}
	b.send(w, "musteriler-"+formatDate(today())+".xlsx")
}

func (s *server) statementXLSX(w http.ResponseWriter, r *http.Request) {
	id, ok := s.customerID(w, r)
	if !ok {
		return
	}
	sp, err := s.statementData(r, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	b := newBook()
	b.open("Ekstre", true, 12, 18, 28, 16, 16, 16)
	b.heading(sp.CustomerName + " — Cari ekstre")
	b.headers("Tarih", "Hareket", "Belge / Yöntem", "Borç", "Alacak", "Bakiye")
	for _, row := range sp.Rows {
		if !row.Date.IsZero() {
			b.cell(1, row.Date, b.date)
		}
		label := map[ledger.RowKind]string{ledger.RowCarried: "Devir", ledger.RowOpening: "Açılış bakiyesi", ledger.RowInvoice: "Fatura", ledger.RowPayment: "Tahsilat"}[row.Kind]
		ref := row.Ref
		if row.Note != "" {
			ref += " · " + row.Note
		}
		b.cell(2, label, 0)
		b.cell(3, ref, 0)
		if row.Debit != 0 {
			b.cell(4, kurus(row.Debit), b.money)
		}
		if row.Credit != 0 {
			b.cell(5, kurus(row.Credit), b.money)
		}
		b.cell(6, kurus(row.Balance), b.money)
		b.row++
	}
	b.cell(3, "Dönem toplamı", b.bold)
	b.cell(4, kurus(sp.TotalDebit), b.bold)
	b.cell(5, kurus(sp.TotalCredit), b.bold)
	b.cell(6, kurus(sp.Position.Balance), b.bold)
	b.send(w, "ekstre-"+fileSafe(sp.CustomerName)+".xlsx")
}
