package cmd

import (
	"fmt"
	"time"

	"github.com/house-of-cards/hoc/internal/store"
	"github.com/house-of-cards/hoc/internal/util"
	"github.com/spf13/cobra"
)

// hansardCmd represents the hansard command.
var hansardCmd = &cobra.Command{
	Use:   "hansard [minister-id]",
	Short: "Hansard（议事录）— 查看部长工作履历",
	Long:  "查看议事录。不带参数显示全部记录；带部长 ID 显示该部长的工作历史。",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		if len(args) > 0 {
			return showMinisterHansard(args[0])
		}
		return listAllHansard()
	},
}

//nolint:gochecknoinits // Cobra convention: register subcommands in init().
func init() {
	hansardCmd.AddCommand(hansardListCmd)
	hansardCmd.AddCommand(hansardTrendCmd)
	hansardCmd.AddCommand(hansardScoreCmd)
	hansardCmd.AddCommand(hansardMetricsCmd) // Phase 4: B-1.4
	hansardTrendCmd.Flags().Int("last", 10, "分析最近 N 条 Hansard 记录（每位部长）")
}

var hansardListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有议事录",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()
		return listAllHansard()
	},
}

func listAllHansard() error {
	entries, err := db.ListHansard()
	if err != nil {
		return fmt.Errorf("list hansard: %w", err)
	}

	if len(entries) == 0 {
		fmt.Println("暂无议事录。议案通过后会自动记录。")
		return nil
	}

	fmt.Printf("📜 议事录（共 %d 条）:\n", len(entries))
	fmt.Println("─────────────────────────────────────────────────")
	for _, h := range entries {
		outcomeIcon := "⚪"
		switch h.Outcome.String {
		case "enacted":
			outcomeIcon = "✅"
		case "failed":
			outcomeIcon = "❌"
		case "partial":
			outcomeIcon = "🟡"
		}
		qualStr := ""
		if h.Quality > 0 {
			qualStr = fmt.Sprintf("  质量: %.2f", h.Quality)
		}
		durStr := ""
		if h.DurationS > 0 {
			durStr = fmt.Sprintf("  耗时: %s", formatDuration(h.DurationS))
		}
		fmt.Printf("%s [%s] %s → %s%s%s\n",
			outcomeIcon,
			h.Outcome.String,
			h.MinisterID,
			h.BillID,
			qualStr,
			durStr,
		)
		fmt.Printf("   %s\n", h.CreatedAt.Format("2006-01-02 15:04"))
		if h.Notes.String != "" {
			fmt.Printf("   备注: %s\n", truncate(h.Notes.String, 80))
		}
	}
	return nil
}

func showMinisterHansard(ministerID string) error {
	// Check minister exists.
	m, err := db.GetMinister(ministerID)
	if err != nil {
		return fmt.Errorf("minister not found: %s", ministerID)
	}

	entries, err := db.ListHansardByMinister(ministerID)
	if err != nil {
		return fmt.Errorf("list hansard: %w", err)
	}

	enacted, total, _ := db.HansardSuccessRate(ministerID)

	fmt.Printf("📜 %s 的议事录（工作履历）\n", m.Title)
	fmt.Println("─────────────────────────────────────────────────")
	fmt.Printf("ID:      %s\n", m.ID)
	fmt.Printf("技能:    %s\n", orDash(m.Skills))
	fmt.Printf("状态:    %s\n", m.Status)
	fmt.Printf("Hansard: %d 条记录\n", len(entries))
	if total > 0 {
		rate := float64(enacted) / float64(total) * 100
		fmt.Printf("成功率:  %d/%d (%.0f%%)\n", enacted, total, rate)
	}
	fmt.Println()

	if len(entries) == 0 {
		fmt.Println("暂无议事录。")
		return nil
	}

	for _, h := range entries {
		outcomeIcon := "⚪"
		switch h.Outcome.String {
		case "enacted":
			outcomeIcon = "✅"
		case "failed":
			outcomeIcon = "❌"
		case "partial":
			outcomeIcon = "🟡"
		}
		fmt.Printf("%s 议案 [%s]", outcomeIcon, h.BillID)
		if h.Quality > 0 {
			fmt.Printf("  质量: %.2f", h.Quality)
		}
		if h.DurationS > 0 {
			fmt.Printf("  耗时: %s", formatDuration(h.DurationS))
		}
		fmt.Printf("  %s\n", h.CreatedAt.Format("2006-01-02 15:04"))
		if h.Notes.String != "" {
			fmt.Printf("   备注: %s\n", h.Notes.String)
		}
	}
	return nil
}

var hansardTrendCmd = &cobra.Command{
	Use:   "trend",
	Short: "显示各部长成功率趋势 ASCII 条形图",
	Long:  "分析最近 N 条 Hansard 记录，渲染每位部长的成功率条形图。",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		lastN, _ := cmd.Flags().GetInt("last")
		return showHansardTrend(lastN)
	},
}

func showHansardTrend(lastN int) error {
	ministers, err := db.ListMinisters()
	if err != nil {
		return fmt.Errorf("list ministers: %w", err)
	}

	if len(ministers) == 0 {
		fmt.Println("暂无部长。")
		return nil
	}

	fmt.Printf("📊 Hansard 成功率趋势 (最近 %d 条/部长)\n", lastN)
	fmt.Println("─────────────────────────────────────────────────")

	var items []util.BarItem
	for _, m := range ministers {
		entries, err := db.ListHansardByMinister(m.ID)
		if err != nil {
			continue
		}
		if len(entries) == 0 {
			continue
		}
		// Take at most lastN entries (already sorted newest-first by DB).
		if len(entries) > lastN {
			entries = entries[:lastN]
		}
		enacted := 0
		for _, h := range entries {
			if h.Outcome.String == "enacted" {
				enacted++
			}
		}
		items = append(items, util.BarItem{
			Label:   truncate(m.Title, 22),
			Success: enacted,
			Total:   len(entries),
		})
	}

	if len(items) == 0 {
		fmt.Println("暂无议事录数据。")
		return nil
	}

	fmt.Print(util.RenderBarChart(items, 20))
	return nil
}

// ─── hansard score ──────────────────────────────────────────────────────────

var hansardScoreCmd = &cobra.Command{
	Use:   "score",
	Short: "显示各部长质量评分排名",
	Long:  "分析 Hansard 数据，按平均质量分（0.0–1.0）排名所有部长，并列出各 portfolio 表现。",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()
		return showHansardScore()
	},
}

func showHansardScore() error {
	ministers, err := db.ListMinisters()
	if err != nil {
		return fmt.Errorf("list ministers: %w", err)
	}

	if len(ministers) == 0 {
		fmt.Println("暂无部长。")
		return nil
	}

	type scoreEntry struct {
		ministerID string
		title      string
		avgQuality float64
		enacted    int
		total      int
		rate       float64
	}

	var entries []scoreEntry
	for _, m := range ministers {
		avg, _ := db.GetMinisterAvgQuality(m.ID)
		enacted, total, _ := db.HansardSuccessRate(m.ID)
		rate := 0.0
		if total > 0 {
			rate = float64(enacted) / float64(total) * 100
		}
		entries = append(entries, scoreEntry{
			ministerID: m.ID,
			title:      m.Title,
			avgQuality: avg,
			enacted:    enacted,
			total:      total,
			rate:       rate,
		})
	}

	// Sort by avgQuality descending.
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].avgQuality > entries[i].avgQuality {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	fmt.Println("🏆 Hansard 质量评分排名")
	fmt.Println("─────────────────────────────────────────────────────")
	fmt.Printf("  %-3s  %-26s  %-8s  %-14s  %s\n", "排名", "部长", "质量分", "成功率", "Hansard")
	fmt.Println("  ─────────────────────────────────────────────────────")

	medals := []string{"🥇", "🥈", "🥉"}
	for i, e := range entries {
		rank := fmt.Sprintf("#%d", i+1)
		if i < len(medals) {
			rank = medals[i]
		}

		qualBar := buildQualityBar(e.avgQuality, 10)
		rateStr := "—"
		if e.total > 0 {
			rateStr = fmt.Sprintf("%d/%d(%.0f%%)", e.enacted, e.total, e.rate)
		}
		qualStr := "—"
		if e.total > 0 {
			qualStr = fmt.Sprintf("%.3f", e.avgQuality)
		}

		fmt.Printf("  %-4s %-26s  %s %s  %-14s  %d条\n",
			rank,
			truncate(e.title, 26),
			qualBar,
			qualStr,
			rateStr,
			e.total,
		)
	}

	fmt.Println()
	fmt.Println("📊 说明: 质量分 = outcome(0.8/0.4/0) + 委员会通过(+0.15) + 无补选(+0.05)")
	fmt.Println("         分数范围 0.0（失败）~ 1.0（完美通过）")
	return nil
}

// buildQualityBar renders a short visual bar for a quality score in [0,1].
func buildQualityBar(q float64, width int) string {
	if q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	filled := int(q * float64(width))
	bar := "[" + repeat("█", filled) + repeat("░", width-filled) + "]"
	return bar
}

func formatDuration(seconds int) string {
	d := time.Duration(seconds) * time.Second
	if d < time.Minute {
		return fmt.Sprintf("%ds", seconds)
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// ─── Phase 4: B-1.4 Hansard Metrics ────────────────────────────────────────

var hansardMetricsCmd = &cobra.Command{
	Use:   "metrics [session-id]",
	Short: "显示质询度量（Question Time Metrics）",
	Long:  "分析 ACK 轮次、首次 ACK 率和简报耗时。不指定 session-id 时显示全局数据。",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		fmt.Println("📊 质询度量 (Question Time Metrics)")
		fmt.Println("═══════════════════════════════════")

		if len(args) == 1 {
			return showSessionMetrics(args[0])
		}
		return showGlobalMetrics()
	},
}

func showSessionMetrics(sessionID string) error {
	s, err := db.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	fmt.Printf("  会期: \"%s\" [%s]\n\n", s.Title, s.ID)

	// First ACK rate.
	firstACKRate, err := db.FirstACKRate(sessionID)
	if err != nil {
		return fmt.Errorf("first ACK rate: %w", err)
	}

	bills, _ := db.ListBillsBySession(sessionID)
	enactedCount := 0
	totalRounds := 0
	totalBriefingS := 0
	briefingCount := 0
	for _, b := range bills {
		if b.Status == "enacted" || b.Status == "royal_assent" {
			enactedCount++
			rounds, _ := db.CountACKRoundsForBill(b.ID)
			totalRounds += rounds
		}
	}

	// Compute briefing time from hansard.
	hansards, _ := db.ListHansardBySession(sessionID)
	for _, h := range hansards {
		if h.BriefingTimeS > 0 {
			totalBriefingS += h.BriefingTimeS
			briefingCount++
		}
	}

	zeroRounds := int(firstACKRate * float64(enactedCount))
	fmt.Printf("  首次 ACK 率:    %d/%d (%.0f%%)\n", zeroRounds, enactedCount, firstACKRate*100)
	if enactedCount > 0 {
		fmt.Printf("  平均 ACK 轮次:  %.1f\n", float64(totalRounds)/float64(enactedCount))
	}
	if briefingCount > 0 {
		avgBriefing := totalBriefingS / briefingCount
		fmt.Printf("  平均简报耗时:   %s\n", formatDuration(avgBriefing))
	}

	// Per-minister metrics.
	return showMinisterMetrics(sessionID)
}

func showGlobalMetrics() error {
	ministers, err := db.ListMinisters()
	if err != nil {
		return fmt.Errorf("list ministers: %w", err)
	}

	if len(ministers) == 0 {
		fmt.Println("  暂无部长数据。")
		return nil
	}

	fmt.Println()
	fmt.Println("  部长表现:")
	for _, m := range ministers {
		firstRate, _ := db.GetMinisterFirstACKRate(m.ID)
		entries, _ := db.ListHansardByMinister(m.ID)
		if len(entries) == 0 {
			continue
		}
		totalRounds := 0
		for _, h := range entries {
			if h.Outcome.String == "enacted" {
				rounds, _ := db.CountACKRoundsForBill(h.BillID)
				totalRounds += rounds
			}
		}
		enacted := 0
		for _, h := range entries {
			if h.Outcome.String == "enacted" {
				enacted++
			}
		}
		avgRounds := 0.0
		if enacted > 0 {
			avgRounds = float64(totalRounds) / float64(enacted)
		}
		fmt.Printf("  %-22s 首次ACK %.0f%%  平均轮次 %.1f\n",
			truncate(m.Title, 22),
			firstRate*100,
			avgRounds,
		)
	}
	return nil
}

func showMinisterMetrics(sessionID string) error {
	hansards, _ := db.ListHansardBySession(sessionID)
	if len(hansards) == 0 {
		return nil
	}

	// Collect unique ministers.
	seen := make(map[string]bool)
	var ministerIDs []string
	for _, h := range hansards {
		if !seen[h.MinisterID] {
			seen[h.MinisterID] = true
			ministerIDs = append(ministerIDs, h.MinisterID)
		}
	}

	fmt.Println()
	fmt.Println("  部长表现:")
	for _, mid := range ministerIDs {
		firstRate, _ := db.GetMinisterFirstACKRate(mid)
		var totalRounds, enacted int
		for _, h := range hansards {
			if h.MinisterID == mid && h.Outcome.String == "enacted" {
				enacted++
				rounds, _ := db.CountACKRoundsForBill(h.BillID)
				totalRounds += rounds
			}
		}
		avgRounds := 0.0
		if enacted > 0 {
			avgRounds = float64(totalRounds) / float64(enacted)
		}
		fmt.Printf("  %-22s 首次ACK %.0f%%  平均轮次 %.1f\n",
			truncate(mid, 22), firstRate*100, avgRounds)
	}
	return nil
}

// Ensure store import is used.
var _ = store.GazetteQuestion
