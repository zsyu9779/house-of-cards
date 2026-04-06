package whip

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/house-of-cards/hoc/internal/store"
	"github.com/house-of-cards/hoc/internal/util"
)

// ─── Gazette Dispatch (unified file inbox) ──────────────────────────────────

// gazetteDispatch routes unread Gazettes.
// For targeted Gazettes (to_minister set): writes to the minister's chamber inbox.
// Broadcast Gazettes are only logged.
func (w *Whip) gazetteDispatch() {
	gazettes, err := w.db.ListUnreadGazettes()
	if err != nil {
		slog.Warn("gazetteDispatch: list", "err", err)
		return
	}

	for _, g := range gazettes {
		to := g.ToMinister.String
		if to == "" {
			to = "(broadcast)"
		}
		slog.Debug("投递公报", "type", g.Type.String, "to", to, "summary", util.Truncate(g.Summary, 60))

		if g.ToMinister.String != "" {
			w.deliverGazette(g)
		}

		if err := w.db.MarkGazetteRead(g.ID); err != nil {
			slog.Warn("gazetteDispatch: mark read", "gazette_id", g.ID, "err", err)
		}
	}
}

// deliverGazette delivers a gazette to its target minister via file inbox.
// Writes to <chamber>/.hoc/inbox/<gazette-id>.md and creates a gazette-signal marker.
func (w *Whip) deliverGazette(g *store.Gazette) {
	ministerID := g.ToMinister.String
	if ministerID == "" {
		return
	}

	minister, err := w.db.GetMinister(ministerID)
	if err != nil || minister.Worktree.String == "" {
		return
	}

	inboxDir := filepath.Join(minister.Worktree.String, ".hoc", "inbox")
	if err := os.MkdirAll(inboxDir, 0755); err != nil {
		slog.Warn("deliverGazette: create inbox dir", "err", err)
		return
	}

	content := fmt.Sprintf("# 公报\n\n**来自**: %s\n**类型**: %s\n**时间**: %s\n\n---\n\n%s\n",
		util.OrDash(g.FromMinister.String),
		util.OrDash(g.Type.String),
		g.CreatedAt.Format("2006-01-02 15:04:05"),
		g.Summary,
	)

	// Append structured payload section if available.
	if g.Payload != "" {
		content += "\n---\n\n## 结构化数据\n\n```json\n" + g.Payload + "\n```\n"
	}

	filename := filepath.Join(inboxDir, g.ID+".md")
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		slog.Warn("deliverGazette: write inbox file", "err", err)
		return
	}

	// Write gazette-signal marker to notify the minister's agent.
	signalPath := filepath.Join(minister.Worktree.String, ".hoc", "gazette-signal")
	_ = os.WriteFile(signalPath, []byte{}, 0644) // best-effort: gazette 信号文件

	slog.Debug("公报已写入 inbox", "minister", ministerID, "file", filename)
	if err := w.db.RecordEvent("gazette.delivered", "whip", g.BillID.String, ministerID, "", fmt.Sprintf(`{"method":"inbox","gazette_id":"%s"}`, g.ID)); err != nil {
		slog.Warn("记录 gazette.delivered 事件失败", "gazette_id", g.ID, "err", err)
	}
}
