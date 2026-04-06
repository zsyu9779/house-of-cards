package whip

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/house-of-cards/hoc/internal/speaker"
	"github.com/house-of-cards/hoc/internal/store"
	"github.com/house-of-cards/hoc/internal/util"
)

// ─── Hansard Update ──────────────────────────────────────────────────────────

// hansardUpdate runs every 60 seconds: logs a status snapshot and refreshes
// the Speaker context.md.
func (w *Whip) hansardUpdate() {
	ministers, _ := w.db.ListMinisters()
	working, idle, stuck := 0, 0, 0
	for _, m := range ministers {
		switch m.Status {
		case "working":
			working++
		case "idle":
			idle++
		case "stuck":
			stuck++
		}
	}

	sessions, _ := w.db.ListActiveSessions()
	slog.Info("Hansard 快照", "active_sessions", len(sessions), "working", working, "idle", idle, "stuck", stuck)

	// Refresh Speaker context.md.
	if w.hocDir != "" {
		if content, err := speaker.GenerateContext(w.db); err == nil {
			ctxPath := filepath.Join(w.hocDir, ".hoc", "speaker", "context.md")
			dirPath := filepath.Dir(ctxPath)
			if mkErr := os.MkdirAll(dirPath, 0755); mkErr == nil {
				if writeErr := os.WriteFile(ctxPath, []byte(content), 0644); writeErr == nil {
					slog.Debug("Speaker context.md 已更新")
				}
			}
		}
	}
}

// ─── Report ──────────────────────────────────────────────────────────────────

// Report returns a human-readable status string for `hoc whip report`.
// If showHistory is true, the last 10 Hansard entries are appended as an event log.
func Report(db *store.DB, showHistory bool) (string, error) {
	var sb strings.Builder

	sessions, err := db.ListActiveSessions()
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}

	allSessions, err := db.ListSessions()
	if err != nil {
		return "", fmt.Errorf("list all sessions: %w", err)
	}

	ministers, err := db.ListMinisters()
	if err != nil {
		return "", fmt.Errorf("list ministers: %w", err)
	}

	gazettes, err := db.ListUnreadGazettes()
	if err != nil {
		return "", fmt.Errorf("list gazettes: %w", err)
	}

	// Aggregate stats.
	wstats, err := db.GetWhipStats()
	if err != nil {
		return "", fmt.Errorf("get whip stats: %w", err)
	}

	// Header
	sb.WriteString("═══════════════════════════════════════\n")
	sb.WriteString("  党鞭状态报告 (Whip Report)\n")
	sb.WriteString(fmt.Sprintf("  %s\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString("═══════════════════════════════════════\n\n")

	// Historical stats.
	avgStr := "—"
	if wstats.AvgDurationS > 0 {
		avgStr = fmtSeconds(wstats.AvgDurationS)
	}
	sb.WriteString("📊 历史统计:\n")
	sb.WriteString(fmt.Sprintf("   补选次数 (By-elections): %d\n", wstats.ByElectionCount))
	sb.WriteString(fmt.Sprintf("   平均完成时长:            %s\n", avgStr))
	if len(wstats.StuckMinisters) > 0 {
		sb.WriteString(fmt.Sprintf("   当前卡住部长:            %d 位\n", len(wstats.StuckMinisters)))
		for _, m := range wstats.StuckMinisters {
			stuckFor := "—"
			if m.Heartbeat.Valid {
				stuckFor = time.Since(m.Heartbeat.Time).Round(time.Second).String()
			}
			sb.WriteString(fmt.Sprintf("     🔴 %s — 卡住 %s\n", m.Title, stuckFor))
		}
	} else {
		sb.WriteString("   当前卡住部长:            无\n")
	}

	// Sessions
	sb.WriteString(fmt.Sprintf("\n📋 会期 (Sessions): %d 活跃 / %d 总计\n", len(sessions), len(allSessions)))
	for _, s := range allSessions {
		icon := "🟢"
		if s.Status == "completed" {
			icon = "✅"
		} else if s.Status == "dissolved" {
			icon = "⚫"
		}
		sb.WriteString(fmt.Sprintf("  %s [%s] %s\n", icon, s.Status, s.Title))

		bills, _ := db.ListBillsBySession(s.ID)
		counts := map[string]int{}
		for _, b := range bills {
			counts[b.Status]++
		}
		sb.WriteString(fmt.Sprintf("     议案: draft=%d reading=%d enacted=%d failed=%d\n",
			counts["draft"], counts["reading"], counts["enacted"], counts["failed"]))
	}

	// Ministers
	sb.WriteString(fmt.Sprintf("\n🏛  内阁 (Cabinet): %d 部长\n", len(ministers)))
	for _, m := range ministers {
		icon := "⚪"
		switch m.Status {
		case "working":
			icon = "🟢"
		case "idle":
			icon = "🟡"
		case "stuck":
			icon = "🔴"
		}
		hbStr := "从未"
		if m.Heartbeat.Valid {
			ago := time.Since(m.Heartbeat.Time).Round(time.Second)
			hbStr = fmt.Sprintf("%s 前", ago)
		}
		enacted, total, _ := db.HansardSuccessRate(m.ID)
		rateStr := "-"
		if total > 0 {
			rateStr = fmt.Sprintf("%d/%d", enacted, total)
		}
		sb.WriteString(fmt.Sprintf("  %s %s [%s]  心跳: %s  Hansard: %s\n",
			icon, m.Title, m.Status, hbStr, rateStr))
	}

	// Unread Gazettes
	sb.WriteString(fmt.Sprintf("\n📰 未读公报: %d 份\n", len(gazettes)))
	for _, g := range gazettes {
		sb.WriteString(fmt.Sprintf("  [%s] → %s: %s\n",
			g.Type.String, util.OrDash(g.ToMinister.String), util.Truncate(g.Summary, 60)))
	}

	// Optional history: last 10 Hansard entries.
	if showHistory {
		sb.WriteString("\n📜 最近事件日志 (Last 10 Hansard entries):\n")
		sb.WriteString("─────────────────────────────────────────\n")
		entries, err := db.ListRecentHansard(10)
		switch {
		case err != nil:
			sb.WriteString(fmt.Sprintf("  (获取失败: %v)\n", err))
		case len(entries) == 0:
			sb.WriteString("  (暂无记录)\n")
		default:
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
				durStr := ""
				if h.DurationS > 0 {
					durStr = fmt.Sprintf("  %s", fmtSeconds(float64(h.DurationS)))
				}
				sb.WriteString(fmt.Sprintf("  %s %s → %s%s  %s\n",
					outcomeIcon,
					util.Truncate(h.MinisterID, 20),
					util.Truncate(h.BillID, 20),
					durStr,
					h.CreatedAt.Format("01-02 15:04"),
				))
			}
		}
	}

	sb.WriteString("\n═══════════════════════════════════════\n")
	return sb.String(), nil
}

// fmtSeconds formats a float64 seconds value as a human-readable duration.
func fmtSeconds(s float64) string {
	d := time.Duration(s) * time.Second
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", s)
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
