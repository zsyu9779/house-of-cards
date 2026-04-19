package cmd

import (
	"strings"
	"testing"
	"time"
)

// Tests for pure helper functions in the cmd package. These exercise
// formatter-style helpers that were previously uncovered.

func TestOrDefault(t *testing.T) {
	cases := []struct {
		in, def, want string
	}{
		{"", "fallback", "fallback"},
		{"value", "fallback", "value"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := orDefault(c.in, c.def); got != c.want {
			t.Errorf("orDefault(%q,%q)=%q want %q", c.in, c.def, got, c.want)
		}
	}
}

func TestBillStatusIcon(t *testing.T) {
	seen := map[string]bool{}
	for _, st := range []string{"draft", "reading", "committee", "enacted", "royal_assent", "failed", "epic", "weird-unknown"} {
		icon := billStatusIcon(st)
		if icon == "" {
			t.Errorf("status %q produced empty icon", st)
		}
		seen[st] = true
	}
	// Unknown defaults to a distinct icon.
	if billStatusIcon("") == billStatusIcon("draft") {
		t.Error("empty status should not collide with draft")
	}
}

func TestTopicIcon(t *testing.T) {
	cases := map[string]bool{
		"bill.created":        true,
		"bill.assigned":       true,
		"bill.enacted":        true,
		"minister.stuck":      true,
		"session.completed":   true,
		"by_election.fired":   true,
		"gazette.created":     true,
		"privy.merged":        true,
		"committee.passed":    true,
		"governance.paused":   true,
		"some.unknown.topic":  true,
	}
	for topic := range cases {
		if icon := topicIcon(topic); icon == "" {
			t.Errorf("topic %q produced empty icon", topic)
		}
	}
	// Default branch produces 📌 for unknown.
	if topicIcon("__unknown__") != "📌" {
		t.Errorf("unknown topic should map to 📌")
	}
}

func TestBuildProgressBar(t *testing.T) {
	cases := []struct {
		name        string
		done, total int
		width       int
		wantFilled  int
	}{
		{"zero total", 0, 0, 10, 0},
		{"half", 5, 10, 10, 5},
		{"full", 10, 10, 10, 10},
		{"overflow clamped", 30, 10, 5, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildProgressBar(c.done, c.total, c.width)
			if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
				t.Fatalf("expected bracketed bar, got %q", got)
			}
			filled := strings.Count(got, "█")
			if filled != c.wantFilled {
				t.Errorf("filled=%d want=%d (bar=%q)", filled, c.wantFilled, got)
			}
		})
	}
}

func TestPadRight(t *testing.T) {
	if got := padRight("ab", 5); got != "ab   " {
		t.Errorf("padRight short: %q", got)
	}
	if got := padRight("abcde", 3); got != "abcde" {
		t.Errorf("padRight longer should be untouched: %q", got)
	}
	if got := padRight("abc", 3); got != "abc" {
		t.Errorf("padRight equal should be untouched: %q", got)
	}
}

func TestFmtDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{2 * time.Minute, "2m"},
		{3 * time.Hour, "3h"},
		{90 * time.Minute, "1h"},
	}
	for _, c := range cases {
		if got := fmtDuration(c.d); got != c.want {
			t.Errorf("fmtDuration(%s)=%q want %q", c.d, got, c.want)
		}
	}
}

func TestParsePortfolio(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"valid json array", `["go","ts"]`, []string{"go", "ts"}},
		{"malformed falls back to single entry", `not-json`, []string{"not-json"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parsePortfolio(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("index %d: %q vs %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestMinisterHasPortfolio(t *testing.T) {
	cases := []struct {
		name     string
		skills   string
		pf       string
		expected bool
	}{
		{"empty skills", "", "go", false},
		{"match case-insensitive", `["Go","ts"]`, "go", true},
		{"no match", `["ts","py"]`, "go", false},
		{"malformed but substring match", `raw-go-string`, "go", true},
		{"malformed no substring", `raw-ts-string`, "go", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ministerHasPortfolio(c.skills, c.pf); got != c.expected {
				t.Errorf("ministerHasPortfolio(%q,%q)=%v want %v", c.skills, c.pf, got, c.expected)
			}
		})
	}
}

func TestPortfolioStr(t *testing.T) {
	if got := portfolioStr(nil); got != "(通用)" {
		t.Errorf("nil skills: got %q", got)
	}
	if got := portfolioStr([]string{"go", "ts"}); got != "go, ts" {
		t.Errorf("unexpected join: %q", got)
	}
}

func TestShortID(t *testing.T) {
	id1 := shortID("bill")
	id2 := shortID("bill")
	if !strings.HasPrefix(id1, "bill-") {
		t.Errorf("missing prefix: %q", id1)
	}
	if id1 == id2 {
		t.Errorf("expected distinct ids, got %q twice", id1)
	}
	if len(id1) != len("bill-")+8 { // 4 bytes → 8 hex chars
		t.Errorf("unexpected length for %q", id1)
	}
}

func TestOrDashCmdWrapper(t *testing.T) {
	if orDash("") != "-" {
		t.Error("orDash empty")
	}
	if orDash("x") != "x" {
		t.Error("orDash non-empty")
	}
}

func TestTruncateCmdWrapper(t *testing.T) {
	if truncate("abcdef", 4) != "a..." {
		t.Errorf("truncate short: %q", truncate("abcdef", 4))
	}
}

func TestRepeat(t *testing.T) {
	if repeat("-", 3) != "---" {
		t.Error("repeat failed")
	}
	if repeat("x", 0) != "" {
		t.Error("repeat 0 should be empty")
	}
}
