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
	marginX  = 15.0
	pageW    = 210.0
	contentW = pageW - 2*marginX
)

func tl(kurus int64) string { return money.Format(kurus) + " TL" }

// DraftInvoice renders a draft invoice preview. It is clearly marked as a draft and is not a valid invoice.
func DraftInvoice(d invoice.Draft, c customer.Customer, t invoice.Totals, now time.Time) ([]byte, error) {
	p := fpdf.New("P", "mm", "A4", "")
	p.AddUTF8FontFromBytes(fontName, "", goregular.TTF)
	p.AddUTF8FontFromBytes(fontName, "B", gobold.TTF)
	p.SetMargins(marginX, 15, marginX)
	p.SetAutoPageBreak(true, 20)
	p.SetTitle("Fatura taslağı önizleme", true)
	p.SetCreator("Hesap Kitap", true)

	p.SetFooterFunc(func() {
		p.SetY(-14)
		p.SetFont(fontName, "", 8)
		p.SetTextColor(120, 120, 120)
		p.CellFormat(contentW/2, 5, "Hesap Kitap · taslak önizleme · "+now.Format("02.01.2006 15:04"), "", 0, "L", false, 0, "")
		p.CellFormat(contentW/2, 5, fmt.Sprintf("Sayfa %d", p.PageNo()), "", 0, "R", false, 0, "")
	})
	p.SetHeaderFunc(func() { watermark(p) })
	p.AddPage()

	// Başlık
	p.SetTextColor(30, 27, 75)
	p.SetFont(fontName, "B", 18)
	p.CellFormat(contentW, 9, "FATURA TASLAĞI", "", 1, "L", false, 0, "")
	p.SetFont(fontName, "", 9)
	p.SetTextColor(150, 60, 60)
	p.CellFormat(contentW, 5, "Önizleme: bu belge geçerli bir e-Arşiv / e-Fatura değildir; fatura kesilince Trendyol'dan gelen PDF geçerlidir.", "", 1, "L", false, 0, "")
	p.Ln(4)

	// Alıcı ve tarih bilgileri
	top := p.GetY()
	p.SetTextColor(0, 0, 0)
	box(p, marginX, top, contentW*0.62, "ALICI", receiverLines(c, d))
	box(p, marginX+contentW*0.64, top, contentW*0.36, "FATURA BİLGİSİ", infoLines(d))
	p.SetY(top + 42)

	lineTable(p, d, t)
	totals(p, t)

	if n := strings.TrimSpace(d.Notes); n != "" {
		p.Ln(6)
		p.SetFont(fontName, "B", 9)
		p.SetTextColor(60, 60, 60)
		p.CellFormat(contentW, 5, "Not", "", 1, "L", false, 0, "")
		p.SetFont(fontName, "", 9)
		p.MultiCell(contentW, 4.5, n, "", "L", false)
	}

	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf üretilemedi: %w", err)
	}
	return buf.Bytes(), nil
}

func watermark(p *fpdf.Fpdf) {
	p.SetFont(fontName, "B", 90)
	p.SetTextColor(238, 238, 244)
	p.TransformBegin()
	p.TransformRotate(40, pageW/2, 150)
	p.Text(pageW/2-52, 160, "TASLAK")
	p.TransformEnd()
	p.SetTextColor(0, 0, 0)
}

func receiverLines(c customer.Customer, d invoice.Draft) []string {
	name := c.Name
	if name == "" {
		name = d.CustomerName
	}
	lines := []string{name}
	tax := c.TaxID
	if c.Kind == customer.KindPerson {
		tax = "TCKN: " + tax
	} else {
		tax = "VKN: " + tax
		if c.TaxOffice != "" {
			tax += " · " + c.TaxOffice + " V.D."
		}
	}
	lines = append(lines, tax)
	if c.Address != "" {
		lines = append(lines, c.Address)
	}
	if loc := strings.TrimSpace(c.District + " / " + c.City); loc != "/" {
		lines = append(lines, strings.Trim(loc, " /"))
	}
	if c.Email != "" {
		lines = append(lines, c.Email)
	}
	if c.Phone != "" {
		lines = append(lines, c.Phone)
	}
	return lines
}

func infoLines(d invoice.Draft) []string {
	lines := []string{"Fatura tarihi: " + d.IssueDate.Format("02.01.2006")}
	if d.DueDate != nil {
		lines = append(lines, "Vade: "+d.DueDate.Format("02.01.2006"))
	}
	return lines
}

func box(p *fpdf.Fpdf, x, y, w float64, title string, lines []string) {
	p.SetDrawColor(220, 220, 230)
	p.SetFillColor(248, 248, 252)
	p.RoundedRect(x, y, w, 38, 2, "1234", "DF")
	p.SetXY(x+3, y+2.5)
	p.SetFont(fontName, "B", 8)
	p.SetTextColor(99, 102, 241)
	p.CellFormat(w-6, 4, title, "", 1, "L", false, 0, "")
	for i, l := range lines {
		p.SetX(x + 3)
		if i == 0 && title == "ALICI" {
			p.SetFont(fontName, "B", 10)
		} else {
			p.SetFont(fontName, "", 9)
		}
		p.SetTextColor(20, 20, 20)
		p.MultiCell(w-6, 4.4, l, "", "L", false)
	}
}

func lineTable(p *fpdf.Fpdf, d invoice.Draft, t invoice.Totals) {
	cols := []struct {
		title string
		w     float64
		align string
	}{
		{"#", 8, "C"}, {"Açıklama", 62, "L"}, {"Miktar", 22, "R"}, {"Birim fiyat", 26, "R"}, {"İndirim", 20, "R"}, {"KDV", 12, "R"}, {"Tutar", 30, "R"},
	}
	p.SetFont(fontName, "B", 8.5)
	p.SetFillColor(79, 70, 229)
	p.SetTextColor(255, 255, 255)
	p.SetDrawColor(79, 70, 229)
	for _, c := range cols {
		p.CellFormat(c.w, 7, c.title, "1", 0, c.align, true, 0, "")
	}
	p.Ln(-1)

	p.SetTextColor(20, 20, 20)
	p.SetDrawColor(225, 225, 232)
	p.SetFont(fontName, "", 9)
	for i, l := range d.Lines {
		res := t.Lines[i]
		nameLines := p.SplitText(l.ItemName, cols[1].w-2)
		h := float64(max(len(nameLines), 1)) * 4.6
		if h < 7 {
			h = 7
		}
		if p.GetY()+h > 297-25 {
			p.AddPage()
		}
		x, y := p.GetX(), p.GetY()
		vals := []string{
			fmt.Sprint(i + 1), "", l.Quantity.String() + " " + product.UnitLabel(l.Unit), tl(l.UnitPrice), tl(l.Discount),
			fmt.Sprintf("%%%d", l.TaxPercent), tl(res.Total),
		}
		cx := x
		for ci, c := range cols {
			p.Rect(cx, y, c.w, h, "D")
			if ci == 1 {
				p.SetXY(cx+1, y+1.2)
				p.MultiCell(c.w-2, 4.6, l.ItemName, "", "L", false)
			} else {
				p.SetXY(cx+1, y+(h-4.6)/2)
				p.CellFormat(c.w-2, 4.6, vals[ci], "", 0, c.align, false, 0, "")
			}
			cx += c.w
		}
		p.SetXY(x, y+h)
	}
}

func totals(p *fpdf.Fpdf, t invoice.Totals) {
	p.Ln(5)
	if p.GetY() > 297-70 {
		p.AddPage()
	}
	labelW, valW := 52.0, 34.0
	x := marginX + contentW - labelW - valW
	row := func(label, val string, bold bool) {
		p.SetX(x)
		style := ""
		if bold {
			style = "B"
		}
		p.SetFont(fontName, style, 9.5)
		p.CellFormat(labelW, 6, label, "", 0, "R", false, 0, "")
		p.CellFormat(valW, 6, val, "", 1, "R", false, 0, "")
	}
	p.SetTextColor(40, 40, 40)
	row("Mal/Hizmet toplamı", tl(t.LineExtension), false)
	if t.Allowance > 0 {
		row("Toplam indirim", "-"+tl(t.Allowance), false)
	}
	row("Matrah (KDV hariç)", tl(t.TaxExclusive), false)
	for _, g := range t.TaxGroups {
		row(fmt.Sprintf("KDV %%%d (matrah %s)", g.Percent, tl(g.Taxable)), tl(g.Tax), false)
	}
	row("Toplam KDV", tl(t.TotalTax), false)
	p.SetDrawColor(79, 70, 229)
	p.Line(x, p.GetY()+1, x+labelW+valW, p.GetY()+1)
	p.Ln(2)
	p.SetTextColor(30, 27, 75)
	row("ÖDENECEK TUTAR", tl(t.Payable), true)
}
