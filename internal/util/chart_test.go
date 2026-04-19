package util

import (
	"strings"
	"testing"
)

func TestRenderBarChart(t *testing.T) {
	t.Run("empty returns placeholder", func(t *testing.T) {
		got := RenderBarChart(nil, 20)
		if !strings.Contains(got, "无数据") {
			t.Errorf("expected no-data placeholder, got %q", got)
		}
	})

	t.Run("default width when non-positive", func(t *testing.T) {
		items := []BarItem{{Label: "m1", Success: 5, Total: 10}}
		got := RenderBarChart(items, 0)
		// default width is 20 → bar bracket content is 20 chars.
		if !strings.Contains(got, "  50%") {
			t.Errorf("expected 50%% output, got %q", got)
		}
		if !strings.Contains(got, "(5/10)") {
			t.Errorf("expected (5/10) output, got %q", got)
		}
	})

	t.Run("zero total yields 0 percent", func(t *testing.T) {
		items := []BarItem{{Label: "m1", Success: 0, Total: 0}}
		got := RenderBarChart(items, 10)
		if !strings.Contains(got, "  0%") {
			t.Errorf("expected 0%% output, got %q", got)
		}
	})

	t.Run("label padding across items", func(t *testing.T) {
		items := []BarItem{
			{Label: "a", Success: 1, Total: 1},
			{Label: "longer-name", Success: 1, Total: 2},
		}
		got := RenderBarChart(items, 10)
		lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines, got %d: %q", len(lines), got)
		}
		// Both lines should include both bar brackets.
		for _, ln := range lines {
			if !strings.Contains(ln, "[") || !strings.Contains(ln, "]") {
				t.Errorf("line missing bar brackets: %q", ln)
			}
		}
	})

	t.Run("overflow filled clamped to barWidth", func(t *testing.T) {
		// Force pct > 100 by setting Success > Total.
		items := []BarItem{{Label: "m", Success: 30, Total: 10}}
		got := RenderBarChart(items, 5)
		// Should not panic; filled is clamped.
		if !strings.Contains(got, "[") {
			t.Errorf("expected chart output, got %q", got)
		}
	})
}
