package customer

import (
	"context"
	"fmt"
	"strings"

	"hesapkitap/internal/money"
)

// ImportColumns are the template headings, in order.
var ImportColumns = []string{"Ünvan", "Tür", "VKN/TCKN", "Vergi dairesi", "İl", "İlçe", "Adres", "Posta kodu", "E-posta", "Telefon", "Açılış bakiyesi"}

// ImportResult is the outcome of one spreadsheet row.
type ImportResult struct {
	Row     int // Excel satır numarası (1 = başlık)
	Name    string
	Created bool
	Message string // başarısızsa Türkçe neden
}

// asciiFold lowers a heading for matching only (never used for API values): Turkish letters → ASCII.
var asciiFold = strings.NewReplacer(
	"İ", "i", "I", "i", "ı", "i", "Ş", "s", "ş", "s", "Ğ", "g", "ğ", "g",
	"Ü", "u", "ü", "u", "Ö", "o", "ö", "o", "Ç", "c", "ç", "c",
)

func fold(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(asciiFold.Replace(s)), " "))
}

var headingField = map[string]string{
	"unvan": "name", "ad soyad": "name", "musteri": "name", "unvan / ad soyad": "name",
	"tur":      "kind",
	"vkn/tckn": "tax_id", "vkn": "tax_id", "tckn": "tax_id", "vkn / tckn": "tax_id",
	"vergi dairesi": "tax_office",
	"il":            "city", "sehir": "city",
	"ilce":       "district",
	"adres":      "address",
	"posta kodu": "postal_code",
	"e-posta":    "email", "eposta": "email", "email": "email",
	"telefon":         "phone",
	"acilis bakiyesi": "opening", "bakiye": "opening",
}

// Import reads spreadsheet rows (first row = headings) and creates the customers one by one.
// A bad row never stops the others. Unknown headings are ignored; "Ünvan" and "VKN/TCKN" are required.
func (s *Service) Import(ctx context.Context, rows [][]string) ([]ImportResult, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("dosya boş")
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		if f, ok := headingField[fold(h)]; ok {
			if _, dup := col[f]; !dup {
				col[f] = i
			}
		}
	}
	if _, ok := col["name"]; !ok {
		return nil, fmt.Errorf("\"Ünvan\" sütunu bulunamadı; şablonu indirip kullanın")
	}
	if _, ok := col["tax_id"]; !ok {
		return nil, fmt.Errorf("\"VKN/TCKN\" sütunu bulunamadı; şablonu indirip kullanın")
	}

	var out []ImportResult
	for i, r := range rows[1:] {
		get := func(f string) string {
			if idx, ok := col[f]; ok && idx < len(r) {
				return strings.TrimSpace(r[idx])
			}
			return ""
		}
		if strings.Join(r, "") == "" {
			continue // boş satır
		}
		res := ImportResult{Row: i + 2, Name: get("name")}
		c := Customer{
			Name: res.Name, TaxID: normalizeTaxID(get("tax_id")), TaxOffice: get("tax_office"), City: get("city"), District: get("district"),
			Address: get("address"), PostalCode: get("postal_code"), Email: get("email"), Phone: get("phone"), Active: true,
		}
		switch fold(get("kind")) {
		case "sahis", "sahis (tckn)", "person":
			c.Kind = KindPerson
		case "sirket", "firma", "company":
			c.Kind = KindCompany
		default:
			c.Kind = KindCompany
			if len(c.TaxID) == 11 {
				c.Kind = KindPerson
			}
		}
		if ob := get("opening"); ob != "" {
			kurus, err := money.Parse(ob)
			if err != nil {
				res.Message = "Açılış bakiyesi okunamadı: " + ob
				out = append(out, res)
				continue
			}
			c.OpeningBalance = kurus
		}
		_, verrs, err := s.Create(ctx, c)
		switch {
		case err != nil:
			return out, err
		case len(verrs) > 0:
			res.Message = firstError(verrs)
		default:
			res.Created = true
		}
		out = append(out, res)
	}
	return out, nil
}

// normalizeTaxID undoes what Excel does to numbers: spaces, a trailing ".0", and a lost leading zero of a VKN.
func normalizeTaxID(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	s = strings.TrimSuffix(s, ".0")
	if len(s) == 9 {
		s = "0" + s
	}
	return s
}

// firstError returns the messages in a stable order (field order of the form).
func firstError(e FieldErrors) string {
	var parts []string
	for _, k := range []string{"name", "tax_id", "kind", "city", "district", "tax_office", "address", "postal_code", "email", "phone", "note"} {
		if m, ok := e[k]; ok {
			parts = append(parts, m)
		}
	}
	for k, m := range e {
		found := false
		for _, p := range parts {
			if p == m {
				found = true
			}
		}
		if !found {
			parts = append(parts, k+": "+m)
		}
	}
	return strings.Join(parts, " ")
}
