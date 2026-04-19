package cmd

import (
	"strings"
	"testing"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		s    int
		want string
	}{
		{15, "15s"},
		{90, "1m30s"},
		{3600, "1h0m"},
		{3660, "1h1m"},
	}
	for _, c := range cases {
		if got := formatDuration(c.s); got != c.want {
			t.Errorf("formatDuration(%d)=%q want %q", c.s, got, c.want)
		}
	}
}

func TestBuildQualityBar(t *testing.T) {
	cases := []struct {
		name  string
		q     float64
		width int
		fills int
	}{
		{"zero", 0, 10, 0},
		{"full", 1, 10, 10},
		{"half", 0.5, 10, 5},
		{"negative clamped", -0.2, 4, 0},
		{"over 1 clamped", 1.5, 4, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildQualityBar(c.q, c.width)
			if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
				t.Fatalf("bad format: %q", got)
			}
			if strings.Count(got, "█") != c.fills {
				t.Errorf("fills=%d want %d (bar=%q)", strings.Count(got, "█"), c.fills, got)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"a\nb", []string{"a", "b"}},
		{"a\nb\n", []string{"a", "b"}},
		{"only\n", []string{"only"}},
	}
	for _, c := range cases {
		got := splitLines(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitLines(%q)=%v want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("index %d: %q vs %q", i, got[i], c.want[i])
			}
		}
	}
}
