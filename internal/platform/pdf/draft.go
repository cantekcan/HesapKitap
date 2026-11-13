// Package pdf renders documents generated locally (not the official Trendyol/GİB invoice PDF).
package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/invoice"
	"hesapkitap/internal/money"
	"hesapkitap/internal/product"
)

// Go yazı tipi Türkçe harfleri (ğ ş ı İ ö ü ç) içerir; ₺ işareti içermediği için tutarlar "TL" ile yazılır.
const (
	fontName = "Go"
	marginX  = 10.0
	pageW    = 210.0
	contentW = pageW - 2*marginX
)

// Company is the seller block of the preview. All fields are optional.
type Company struct {
	Name, TaxID, TaxOffice, Address, City, Phone, Email string
}

// DraftData is everything the preview needs.
type DraftData struct {
	Draft    invoice.Draft
	Customer customer.Customer
	Totals   invoice.Totals
	Seller   Company
	EInvoice bool // true: e-Fatura, false: e-Arşiv
	Now      time.Time
}

func tl(kurus int64) string { return money.Format(kurus) + " TL" }

// DraftInvoice renders a draft invoice in the layout of the official GİB e-Arşiv / e-Fatura document,
// marked "TASLAKTIR". It is a preview only and not a valid invoice.
func DraftInvoice(in DraftData) ([]byte, error) {
	p := fpdf.New("P", "mm", "A4", "")
	p.AddUTF8FontFromBytes(fontName, "", goregular.TTF)
	p.AddUTF8FontFromBytes(fontName, "B", gobold.TTF)
	p.SetMargins(marginX, 10, marginX)
	p.SetAutoPageBreak(true, 18)
	p.SetTitle("Fatura taslağı önizleme", true)
	p.SetCreator("Hesap Kitap", true)
	p.SetLineWidth(0.2)

	p.SetFooterFunc(func() {
		p.SetY(-12)
		p.SetFont(fontName, "", 7.5)
		p.SetTextColor(120, 120, 120)
		p.CellFormat(contentW/2, 4, "Taslak önizleme · "+in.Now.Format("02.01.2006 15:04")+" · geçerli bir fatura değildir", "", 0, "L", false, 0, "")
		p.CellFormat(contentW/2, 4, fmt.Sprintf("Sayfa %d", p.PageNo()), "", 0, "R", false, 0, "")
	})
	p.SetHeaderFunc(func() { watermark(p) })
	p.AddPage()

	header(p, in)
	buyerAndInfo(p, in)
	lineTable(p, in.Draft, in.Totals)
	totalsBox(p, in.Totals)
	notesBox(p, in)

	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf üretilemedi: %w", err)
	}
	return buf.Bytes(), nil
}

func watermark(p *fpdf.Fpdf) {
	p.SetFont(fontName, "B", 76)
	p.SetTextColor(250, 214, 214)
	p.TransformBegin()
	p.TransformRotate(40, pageW/2, 160)
	p.Text(pageW/2-50, 170, "TASLAKTIR")
	p.TransformEnd()
	p.SetTextColor(0, 0, 0)
}

// header: satıcı bloğu solda, QR yeri ve belge türü sağda (resmi belgedeki yerleşim).
func header(p *fpdf.Fpdf, in DraftData) {
	top := p.GetY()
	s := in.Seller
	p.SetTextColor(0, 0, 0)
	p.SetXY(marginX, top)
	if strings.TrimSpace(s.Name) == "" {
		p.SetFont(fontName, "", 8.5)
		p.SetTextColor(150, 150, 150)
		p.MultiCell(95, 4.2, "Satıcı bilgileri tanımlı değil.\n(.env: COMPANY_NAME, COMPANY_TAX_ID … ile ekleyebilirsiniz)", "", "L", false)
	} else {
		p.SetFont(fontName, "B", 9.5)
		p.MultiCell(95, 4.6, s.Name, "", "L", false)
		p.SetFont(fontName, "", 8.5)
		for _, l := range sellerLines(s) {
			p.SetX(marginX)
			p.MultiCell(95, 4.1, l, "", "L", false)
		}
	}
	sellerEnd := p.GetY()

	// QR yeri (taslakta QR yok)
	qx, qy, qs := pageW-marginX-30, top, 30.0
	p.SetDrawColor(180, 180, 180)
	p.SetDashPattern([]float64{1.2, 1.2}, 0)
	p.Rect(qx, qy, qs, qs, "D")
	p.SetDashPattern([]float64{}, 0)
	p.SetFont(fontName, "", 6.5)
	p.SetTextColor(150, 150, 150)
	p.SetXY(qx, qy+qs/2-5)
	p.MultiCell(qs, 3.4, "QR kod\nfatura kesilince\noluşur", "", "C", false)

	p.SetFont(fontName, "B", 11)
	p.SetTextColor(60, 60, 60)
	p.SetXY(qx-5, qy+qs+1.5)
	p.CellFormat(qs+5, 5, docTitle(in.EInvoice), "", 0, "C", false, 0, "")

	p.SetY(max(sellerEnd, qy+qs+8) + 3)
	p.SetDrawColor(0, 0, 0)
	p.SetTextColor(0, 0, 0)
	p.SetFont(fontName, "", 8)
	p.CellFormat(contentW, 4.5, "ETTN: Fatura kesildiğinde atanır", "", 1, "L", false, 0, "")
	p.Ln(1.5)
}

func docTitle(einvoice bool) string {
	if einvoice {
		return "e-Fatura"
	}
	return "e-Arşiv Fatura"
}

func sellerLines(s Company) []string {
	var out []string
	if s.Address != "" {
		out = append(out, s.Address)
	}
	if s.City != "" {
		out = append(out, s.City)
	}
	if s.Phone != "" {
		out = append(out, "Tel: "+s.Phone)
	}
	if s.Email != "" {
		out = append(out, "E-Posta: "+s.Email)
	}
	if s.TaxOffice != "" {
		out = append(out, "Vergi Dairesi: "+s.TaxOffice)
	}
	if s.TaxID != "" {
		out = append(out, "VKN: "+s.TaxID)
	}
	return out
}

func buyerAndInfo(p *fpdf.Fpdf, in DraftData) {
	top := p.GetY()
	c, d := in.Customer, in.Draft

	// Alıcı (SAYIN) — çerçevesiz, resmi belgedeki gibi
	p.SetXY(marginX, top)
	p.SetFont(fontName, "B", 8.5)
	p.CellFormat(95, 4.6, "SAYIN", "", 1, "L", false, 0, "")
	name := c.Name
	if name == "" {
		name = d.CustomerName
	}
	p.SetX(marginX)
	p.SetFont(fontName, "B", 9.5)
	p.MultiCell(98, 4.6, name, "", "L", false)
	p.SetFont(fontName, "", 8.5)
	for _, l := range receiverLines(c) {
		p.SetX(marginX)
		p.MultiCell(98, 4.1, l, "", "L", false)
	}
	buyerEnd := p.GetY()

	// Fatura bilgileri tablosu
	scenario := "EARSIVFATURA"
	if in.EInvoice {
		scenario = "TEMELFATURA"
	}
	rows := [][2]string{
		{"Özelleştirme No", "TR1.2"},
		{"Senaryo", scenario},
		{"Fatura Tipi", "SATIS"},
		{"Fatura No", "Kesildiğinde atanır"},
		{"Fatura Tarihi", d.IssueDate.Format("02.01.2006")},
	}
	if d.DueDate != nil {
		rows = append(rows, [2]string{"Vade Tarihi", d.DueDate.Format("02.01.2006")})
	}
	x, w, lw := pageW-marginX-72, 72.0, 27.0
	y := top
	for _, r := range rows {
		p.SetXY(x, y)
		p.SetFont(fontName, "B", 7.8)
		p.CellFormat(lw, 5.2, r[0], "1", 0, "L", false, 0, "")
		p.SetFont(fontName, "", 7.8)
		p.CellFormat(w-lw, 5.2, r[1], "1", 0, "L", false, 0, "")
		y += 5.2
	}
	p.SetY(max(buyerEnd, y) + 4)
}

func receiverLines(c customer.Customer) []string {
	var lines []string
	if c.Address != "" {
		lines = append(lines, c.Address)
	}
	if loc := strings.Trim(strings.TrimSpace(c.District+" / "+c.City), " /"); loc != "" {
		lines = append(lines, loc)
	}
	if c.Phone != "" {
		lines = append(lines, "Tel: "+c.Phone)
	}
	if c.Email != "" {
		lines = append(lines, "E-Posta: "+c.Email)
	}
	if c.Kind == customer.KindPerson {
		lines = append(lines, "TCKN: "+c.TaxID)
	} else {
		if c.TaxOffice != "" {
			lines = append(lines, "Vergi Dairesi: "+c.TaxOffice)
		}
		lines = append(lines, "VKN: "+c.TaxID)
	}
	return lines
}

func lineTable(p *fpdf.Fpdf, d invoice.Draft, t invoice.Totals) {
	cols := []struct {
		title string
		w     float64
		align string
	}{
		{"Sıra No", 11, "C"}, {"Mal Hizmet", 56, "L"}, {"Miktar", 20, "R"}, {"Birim Fiyat", 23, "R"},
		{"İskonto Tutarı", 22, "R"}, {"KDV Oranı", 14, "R"}, {"KDV Tutarı", 20, "R"}, {"Mal Hizmet Tutarı", 24, "R"},
	}
	head := func() {
		p.SetFont(fontName, "B", 7.2)
		p.SetFillColor(238, 238, 238)
		p.SetDrawColor(0, 0, 0)
		p.SetTextColor(0, 0, 0)
		for _, c := range cols {
			p.CellFormat(c.w, 7, c.title, "1", 0, "C", true, 0, "")
		}
		p.Ln(-1)
	}
	head()
	p.SetFont(fontName, "", 8)
	for i, l := range d.Lines {
		res := t.Lines[i]
		nameLines := p.SplitText(l.ItemName, cols[1].w-2)
		h := max(float64(len(nameLines))*4.2+2, 7)
		if p.GetY()+h > 297-22 {
			p.AddPage()
			head()
			p.SetFont(fontName, "", 8)
		}
		x, y := p.GetX(), p.GetY()
		vals := []string{
			fmt.Sprint(i + 1), "", l.Quantity.String() + " " + product.UnitLabel(l.Unit), tl(l.UnitPrice), tl(res.Discount),
			fmt.Sprintf("%%%d", l.TaxPercent), tl(res.Tax), tl(res.Taxable),
		}
		cx := x
		for ci, c := range cols {
			p.Rect(cx, y, c.w, h, "D")
			if ci == 1 {
				p.SetXY(cx+1, y+1)
				p.MultiCell(c.w-2, 4.2, l.ItemName, "", "L", false)
			} else {
				p.SetXY(cx+1, y+(h-4.2)/2)
				p.CellFormat(c.w-2, 4.2, vals[ci], "", 0, c.align, false, 0, "")
			}
			cx += c.w
		}
		p.SetXY(x, y+h)
	}
}

func totalsBox(p *fpdf.Fpdf, t invoice.Totals) {
	p.Ln(4)
	if p.GetY() > 297-75 {
		p.AddPage()
	}
	labelW, valW := 56.0, 30.0
	x := marginX + contentW - labelW - valW
	row := func(label, val string, bold bool) {
		p.SetX(x)
		style := ""
		if bold {
			style = "B"
		}
		p.SetFont(fontName, style, 8.2)
		p.CellFormat(labelW, 5.4, label, "1", 0, "R", false, 0, "")
		p.CellFormat(valW, 5.4, val, "1", 1, "R", false, 0, "")
	}
	p.SetTextColor(0, 0, 0)
	row("Mal Hizmet Toplam Tutarı", tl(t.LineExtension), false)
	row("Toplam İskonto", tl(t.Allowance), false)
	for _, g := range t.TaxGroups {
		row(fmt.Sprintf("Hesaplanan KDV (%%%d)", g.Percent), tl(g.Tax), false)
	}
	row("Vergiler Dahil Toplam Tutar", tl(t.TaxInclusive), false)
	row("Ödenecek Tutar", tl(t.Payable), true)
}

func notesBox(p *fpdf.Fpdf, in DraftData) {
	p.Ln(5)
	if p.GetY() > 297-40 {
		p.AddPage()
	}
	text := "Yalnız: " + amountInWords(in.Totals.Payable)
	if n := strings.TrimSpace(in.Draft.Notes); n != "" {
		text += "\n" + n
	}
	x, y := p.GetX(), p.GetY()
	p.SetFont(fontName, "", 8.5)
	lines := p.SplitText(text, contentW-4)
	h := float64(len(lines))*4.3 + 4
	p.Rect(x, y, contentW, h, "D")
	p.SetXY(x+2, y+2)
	p.MultiCell(contentW-4, 4.3, text, "", "L", false)
	p.SetXY(x, y+h)
}

var (
	ones = []string{"", "BİR", "İKİ", "ÜÇ", "DÖRT", "BEŞ", "ALTI", "YEDİ", "SEKİZ", "DOKUZ"}
	tens = []string{"", "ON", "YİRMİ", "OTUZ", "KIRK", "ELLİ", "ALTMIŞ", "YETMİŞ", "SEKSEN", "DOKSAN"}
)

// amountInWords writes a kuruş amount the way printed Turkish invoices do: "ÜÇ BİN OTUZ TL KIRK DÖRT Kr.".
func amountInWords(kurus int64) string {
	if kurus < 0 {
		kurus = -kurus
	}
	lira, kr := kurus/100, kurus%100
	parts := []string{}
	if lira > 0 || kr == 0 {
		w := numberWords(lira)
		if w == "" {
			w = "SIFIR"
		}
		parts = append(parts, w+" TL")
	}
	if kr > 0 {
		parts = append(parts, numberWords(kr)+" Kr.")
	}
	return strings.Join(parts, " ")
}

// numberWords spells n (up to trilyon) in Turkish capitals; "bir" is dropped before yüz and bin.
func numberWords(n int64) string {
	if n == 0 {
		return ""
	}
	scales := []struct {
		v    int64
		name string
	}{{1_000_000_000_000, "TRİLYON"}, {1_000_000_000, "MİLYAR"}, {1_000_000, "MİLYON"}, {1000, "BİN"}}
	var out []string
	for _, s := range scales {
		if n >= s.v {
			q := n / s.v
			n %= s.v
			if s.v == 1000 && q == 1 {
				out = append(out, s.name)
			} else {
				out = append(out, numberWords(q), s.name)
			}
		}
	}
	if h := n / 100; h > 0 {
		if h > 1 {
			out = append(out, ones[h])
		}
		out = append(out, "YÜZ")
		n %= 100
	}
	if t := n / 10; t > 0 {
		out = append(out, tens[t])
	}
	if o := n % 10; o > 0 {
		out = append(out, ones[o])
	}
	return strings.Join(out, " ")
}
