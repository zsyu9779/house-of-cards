package whip

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/house-of-cards/hoc/internal/store"
)

const (
	// contextHealthWarnRatio is the usage ratio (0–1) at which the Whip sends a
	// reminder gazette asking the minister to checkpoint progress.
	contextHealthWarnRatio = 0.80
	// contextHealthCritRatio is the ratio at which the Whip sends an urgent
	// gazette and records an at-risk event against the minister's active bills.
	contextHealthCritRatio = 0.90
	// contextAlertCooldown suppresses duplicate alerts within the window — the
	// tick runs every 10s but the minister's usage changes slowly, so we back
	// off to avoid gazette storms.
	contextAlertCooldown = 5 * time.Minute
)

// checkContextHealth inspects the latest context_health payload for each
// working minister and, when usage crosses the warn/critical thresholds, sends
// the minister a recovery gazette (and for critical, records an at-risk event
// on the minister's active bills). A per-minister cooldown prevents repeated
// alerts for the same condition.
func (w *Whip) checkContextHealth() {
	working, err := w.db.ListWorkingMinisters()
	if err != nil {
		slog.Warn("checkContextHealth: list working ministers", "err", err)
		return
	}

	for _, m := range working {
		health, err := w.db.GetLatestContextHealth(m.ID)
		if err != nil {
			slog.Warn("checkContextHealth: get latest", "minister_id", m.ID, "err", err)
			continue
		}
		if health == nil || health.TokensLimit <= 0 {
			continue
		}

		ratio := float64(health.TokensUsed) / float64(health.TokensLimit)

		if last, ok := w.lastContextAlert[m.ID]; ok && time.Since(last) < contextAlertCooldown {
			continue
		}

		switch {
		case ratio >= contextHealthCritRatio:
			w.sendContextAlert(m, health, ratio, true)
		case ratio >= contextHealthWarnRatio:
			w.sendContextAlert(m, health, ratio, false)
		}
	}
}

// sendContextAlert creates the recovery gazette (and at-risk event for critical)
// and records the cooldown timestamp. Extracted so tests can cover each branch
// without duplicating the gazette/event boilerplate.
func (w *Whip) sendContextAlert(m *store.Minister, health *store.ContextHealth, ratio float64, critical bool) {
	percent := ratio * 100

	var summary string
	if critical {
		summary = fmt.Sprintf(
			"紧急：你的 context 已使用 %.0f%%（%d/%d tokens）。"+
				"请立即做 checkpoint（写 .done 文件保存当前进度），"+
				"或总结已完成部分并请求拆分 Bill。",
			percent, health.TokensUsed, health.TokensLimit,
		)
		slog.Warn("部长 context 接近上限",
			"minister_id", m.ID,
			"tokens_used", health.TokensUsed,
			"tokens_limit", health.TokensLimit,
			"ratio", fmt.Sprintf("%.1f%%", percent),
		)

		bills, err := w.db.GetBillsByAssignee(m.ID)
		if err != nil {
			slog.Warn("checkContextHealth: get bills", "minister_id", m.ID, "err", err)
		}
		for _, bill := range bills {
			if bill.Status != "reading" {
				continue
			}
			payload := fmt.Sprintf(`{"ratio":%.2f,"tokens_used":%d,"tokens_limit":%d}`,
				ratio, health.TokensUsed, health.TokensLimit)
			if err := w.db.RecordEvent("bill.context_critical", "whip",
				bill.ID, m.ID, bill.SessionID.String, payload,
			); err != nil {
				slog.Warn("记录 context_critical 事件失败", "bill_id", bill.ID, "err", err)
			}
		}
	} else {
		summary = fmt.Sprintf(
			"提醒：你的 context 已使用 %.0f%%（%d/%d tokens, %d turns）。"+
				"建议做一次 checkpoint 保存进度。",
			percent, health.TokensUsed, health.TokensLimit, health.TurnsElapsed,
		)
		slog.Info("部长 context 使用率较高",
			"minister_id", m.ID,
			"ratio", fmt.Sprintf("%.1f%%", percent),
		)
	}

	g := &store.Gazette{
		ID:           gazetteID(),
		FromMinister: store.NullString("whip"),
		ToMinister:   store.NullString(m.ID),
		Type:         store.NullString("recovery"),
		Summary:      summary,
	}
	if err := w.db.CreateGazette(g); err != nil {
		slog.Warn("创建 context gazette 失败", "minister_id", m.ID, "critical", critical, "err", err)
		return
	}

	if w.lastContextAlert == nil {
		w.lastContextAlert = make(map[string]time.Time)
	}
	w.lastContextAlert[m.ID] = time.Now()
}
