package whip

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/house-of-cards/hoc/internal/store"
	"github.com/house-of-cards/hoc/internal/util"
)

// ─── Gazette Dispatch (8-3 enhanced) ─────────────────────────────────────────

// gazetteDispatch routes unread Gazettes.
// For targeted Gazettes (to_minister set): attempts tmux send-keys delivery first,
// falls back to writing an inbox file in the minister's chamber.
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

// deliverGazette delivers a gazette to its target minister.
// Strategy 1: tmux send-keys (if minister has an active tmux session).
// Strategy 2: write to <chamber>/.hoc/inbox/<gazette-id>.md.
func (w *Whip) deliverGazette(g *store.Gazette) {
	ministerID := g.ToMinister.String
	if ministerID == "" {
		return
	}

	// Try tmux delivery first.
	tmuxName := fmt.Sprintf("hoc-%s", ministerID)
	if exec.Command("tmux", "has-session", "-t", tmuxName).Run() == nil {
		msg := fmt.Sprintf("\n[公报 | 来自: %s | 类型: %s]\n%s\n",
			util.OrDash(g.FromMinister.String),
			util.OrDash(g.Type.String),
			g.Summary,
		)
		cmd := exec.Command("tmux", "send-keys", "-t", tmuxName, msg, "")
		if err := cmd.Run(); err == nil {
			slog.Debug("公报已发送至 tmux", "minister", ministerID)
			_ = w.db.RecordEvent("gazette.delivered", "whip", g.BillID.String, ministerID, "", fmt.Sprintf(`{"method":"tmux","gazette_id":"%s"}`, g.ID))
			return
		}
	}

	// Fallback: write to chamber inbox.
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
	filename := filepath.Join(inboxDir, g.ID+".md")
	if err := os.WriteFile(filename, []byte(content), 0644); err == nil {
		slog.Debug("公报已写入 inbox", "minister", ministerID, "file", filename)
		_ = w.db.RecordEvent("gazette.delivered", "whip", g.BillID.String, ministerID, "", fmt.Sprintf(`{"method":"inbox","gazette_id":"%s"}`, g.ID))
	}
}
