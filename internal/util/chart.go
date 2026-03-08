package util

import (
	"fmt"
	"strings"
)

// BarItem represents one row in a horizontal bar chart.
type BarItem struct {
	Label   string
	Success int
	Total   int
}

// RenderBarChart renders an ASCII horizontal bar chart.
// barWidth controls the character width of the filled bar.
// Example output:
//
//	backend-minister  [████████░░░░░░░░░░░░]   40%  (4/10)
//	frontend-minister [████████████░░░░░░░░]   60%  (6/10)
func RenderBarChart(items []BarItem, barWidth int) string {
	if len(items) == 0 {
		return "  (无数据)\n"
	}
	if barWidth <= 0 {
		barWidth = 20
	}

	// Find max label width for alignment.
	maxLabel := 0
	for _, item := range items {
		if n := len([]rune(item.Label)); n > maxLabel {
			maxLabel = n
		}
	}

	var sb strings.Builder
	for _, item := range items {
		// Pad label to max width.
		labelRunes := []rune(item.Label)
		for len(labelRunes) < maxLabel {
			labelRunes = append(labelRunes, ' ')
		}
		label := string(labelRunes)

		pct := 0
		filled := 0
		if item.Total > 0 {
			pct = int(float64(item.Success) / float64(item.Total) * 100)
			filled = pct * barWidth / 100
		}
		if filled > barWidth {
			filled = barWidth
		}

		bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled) + "]"
		sb.WriteString(fmt.Sprintf("  %s  %s  %3d%%  (%d/%d)\n",
			label, bar, pct, item.Success, item.Total))
	}
	return sb.String()
}
