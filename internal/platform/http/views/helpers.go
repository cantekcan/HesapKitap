package views

import (
	"encoding/json"
	"net/url"
)

func templURLEscape(s string) string { return url.QueryEscape(s) }

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
