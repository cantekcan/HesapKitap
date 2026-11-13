package views

import (
	"encoding/json"
	"net/url"
	"strings"

	"hesapkitap/internal/customer"
)

func templURLEscape(s string) string { return url.QueryEscape(s) }

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// hasFieldErrors is true when the form has errors other than the "issue" message (which is shown on its own).
func hasFieldErrors(f InvoiceForm) bool {
	n := len(f.Errors)
	if f.Errors["issue"] != "" {
		n--
	}
	return n > 0
}

func importColumnsText() string { return strings.Join(customer.ImportColumns, ", ") }
