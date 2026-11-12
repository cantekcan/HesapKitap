package views

import "net/url"

func templURLEscape(s string) string { return url.QueryEscape(s) }
