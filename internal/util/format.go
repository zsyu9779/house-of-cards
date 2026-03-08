// Package util provides shared formatting helpers for House of Cards CLI.
package util

// OrDash returns s, or "-" if s is empty.
func OrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Truncate truncates s to at most max runes. If truncated, the last 3 runes are "...".
func Truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}
