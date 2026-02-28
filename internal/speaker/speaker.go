// Package speaker implements the Speaker (议长) role: the AI orchestrator
// that receives high-level goals, decomposes them into Bills, and monitors
// overall session progress via a persistent context.
package speaker

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/house-of-cards/hoc/internal/store"
)

const contextRelPath = ".hoc/speaker/context.md"

// ContextData holds all the data needed to render a Speaker context.
type ContextData struct {
	GeneratedAt    time.Time
	ActiveSessions []*SessionSummary
	AllSessions    []*store.Session
	Ministers      []*MinisterSummary
	RecentGazettes []*store.Gazette
}

// SessionSummary aggregates a session with its bill counts.
type SessionSummary struct {
	Session *store.Session
	Bills   []*store.Bill
	Done    int
	Total   int
}

// MinisterSummary aggregates a minister with Hansard stats.
type MinisterSummary struct {
	Minister *store.Minister
	Enacted  int
	Total    int
}

// GenerateContext builds the Speaker context.md content from live DB state.
func GenerateContext(db *store.DB) (string, error) {
	data, err := collectContextData(db)
	if err != nil {
		return "", err
	}
	return renderContext(data), nil
}

// WriteContext writes the generated context to the standard path inside hocDir.
func WriteContext(hocDir, content string) error {
	dir := filepath.Join(hocDir, ".hoc", "speaker")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create speaker dir: %w", err)
	}
	path := filepath.Join(hocDir, contextRelPath)
	return os.WriteFile(path, []byte(content), 0644)
}

// ContextPath returns the absolute path to the Speaker context file.
func ContextPath(hocDir string) string {
	return filepath.Join(hocDir, contextRelPath)
}

// Summon starts the Speaker AI session with the current context injected.
// useTmux=true runs in a detached tmux session named "hoc-speaker".
// useTmux=false runs interactively in the foreground.
func Summon(hocDir string, useTmux bool) error {
	ctxPath := ContextPath(hocDir)
	if _, err := os.Stat(ctxPath); os.IsNotExist(err) {
		return fmt.Errorf("Speaker context 文件不存在：%s\n请先运行 hoc speaker context --refresh", ctxPath)
	}

	if useTmux {
		tmuxName := "hoc-speaker"
		_ = exec.Command("tmux", "kill-session", "-t", tmuxName).Run()

		// Build the speaker prompt path relative to the hocDir.
		speakerPromptPath := filepath.Join(hocDir, ".hoc", "speaker", "prompt.md")
		if err := writeSpeakerPrompt(speakerPromptPath); err != nil {
			return fmt.Errorf("write speaker prompt: %w", err)
		}

		shellCmd := fmt.Sprintf("claude -p @%s", speakerPromptPath)
		cmd := exec.Command("tmux", "new-session", "-d",
			"-s", tmuxName,
			"-c", hocDir,
			shellCmd,
		)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("tmux start: %w\n%s", err, string(output))
		}
		fmt.Printf("✅ 议长已在 tmux 会话 [hoc-speaker] 中就绪\n")
		fmt.Printf("   查看: tmux attach -t hoc-speaker\n")
	} else {
		// Interactive foreground mode.
		speakerPromptPath := filepath.Join(hocDir, ".hoc", "speaker", "prompt.md")
		if err := writeSpeakerPrompt(speakerPromptPath); err != nil {
			return fmt.Errorf("write speaker prompt: %w", err)
		}

		cmd := exec.Command("claude", "-p", "@"+speakerPromptPath)
		cmd.Dir = hocDir
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return nil
}

// writeSpeakerPrompt writes the speaker's initial system prompt which includes
// the context file reference and role instructions.
func writeSpeakerPrompt(path string) error {
	content := fmt.Sprintf(`# 议长就职简报（Speaker Briefing）

> 你是 House of Cards 多 Agent 协作框架的**议长（Speaker）**。
> 你的职责是：接收人类需求、拆解为 Bill（议案）、选择协作拓扑、监督 Session 进度、基于 Gazette 公报做决策。

---

## 政府当前状态

> 以下是最新的政府状态快照（来自 %s）：

{{include: %s}}

---

## 议长职责

1. **拆解任务**：将人类的需求拆解为具体的 Bills，为每个 Bill 指定：
   - title（标题）、motion（指示内容）、portfolio（所需技能）、depends_on（依赖关系）
   - 输出格式：TOML（可直接用于 hoc session open）

2. **选择拓扑**：根据任务特性选择：
   - parallel（并行）：前后端同时开工
   - pipeline（流水线）：逐步依赖
   - tree（树形）：多任务汇合

3. **监督进度**：根据公报（Gazette）判断各部长工作状态，必要时建议补选或重新分配

4. **决策记录**：每次重要决策写入当前对话，供未来 Whip 汇报时参考

---

## 工作指令

等待人类输入。你可以：
- 起草新的 Session TOML 配置
- 分析当前进度并给出建议
- 审阅公报并决定下一步行动

*议长就绪，请指示。*
`, time.Now().Format("2006-01-02 15:04:05"), filepath.Dir(path)+"/context.md")
	return os.WriteFile(path, []byte(content), 0644)
}

// ─── Data Collection ─────────────────────────────────────────────────────────

func collectContextData(db *store.DB) (*ContextData, error) {
	data := &ContextData{GeneratedAt: time.Now()}

	allSessions, err := db.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	data.AllSessions = allSessions

	for _, sess := range allSessions {
		bills, _ := db.ListBillsBySession(sess.ID)
		done := 0
		for _, b := range bills {
			if b.Status == "enacted" || b.Status == "royal_assent" {
				done++
			}
		}
		sum := &SessionSummary{Session: sess, Bills: bills, Done: done, Total: len(bills)}
		data.ActiveSessions = append(data.ActiveSessions, sum)
	}

	ministers, err := db.ListMinisters()
	if err != nil {
		return nil, fmt.Errorf("list ministers: %w", err)
	}

	for _, m := range ministers {
		enacted, total, _ := db.HansardSuccessRate(m.ID)
		data.Ministers = append(data.Ministers, &MinisterSummary{
			Minister: m,
			Enacted:  enacted,
			Total:    total,
		})
	}

	gazettes, _ := db.ListGazettes()
	if len(gazettes) > 10 {
		gazettes = gazettes[:10] // Keep only the 10 most recent.
	}
	data.RecentGazettes = gazettes

	return data, nil
}

// ─── Context Rendering ────────────────────────────────────────────────────────

func renderContext(data *ContextData) string {
	var sb strings.Builder

	sb.WriteString("# Speaker Context（议长备忘录）\n\n")
	sb.WriteString(fmt.Sprintf("> 生成时间：%s\n\n", data.GeneratedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString("---\n\n")

	// Government overview.
	sb.WriteString("## 政府现状\n\n")
	activeSessions := 0
	for _, s := range data.AllSessions {
		if s.Status == "active" {
			activeSessions++
		}
	}
	working, idle, stuck, offline := 0, 0, 0, 0
	for _, ms := range data.Ministers {
		switch ms.Minister.Status {
		case "working":
			working++
		case "idle":
			idle++
		case "stuck":
			stuck++
		default:
			offline++
		}
	}
	sb.WriteString(fmt.Sprintf("- 活跃会期：%d 个（共 %d 个）\n", activeSessions, len(data.AllSessions)))
	sb.WriteString(fmt.Sprintf("- 内阁：%d 位部长（工作中 %d / 待命 %d / 卡住 %d / 离线 %d）\n\n",
		len(data.Ministers), working, idle, stuck, offline))

	// Sessions detail.
	sb.WriteString("## 会期进度\n\n")
	if len(data.ActiveSessions) == 0 {
		sb.WriteString("暂无会期。\n\n")
	}
	for _, ss := range data.ActiveSessions {
		icon := "🟢"
		if ss.Session.Status == "completed" {
			icon = "✅"
		} else if ss.Session.Status == "dissolved" {
			icon = "⚫"
		}
		sb.WriteString(fmt.Sprintf("### %s %s [%s] (%d/%d Bills)\n\n",
			icon, ss.Session.Title, ss.Session.Status, ss.Done, ss.Total))

		for _, b := range ss.Bills {
			statusIcon := billIcon(b.Status)
			assignee := b.Assignee.String
			if assignee == "" {
				assignee = "未分配"
			}
			sb.WriteString(fmt.Sprintf("- %s **%s** → %s  `[%s]`\n",
				statusIcon, b.Title, assignee, b.Status))
		}
		sb.WriteString("\n")
	}

	// Cabinet directory.
	sb.WriteString("## 内阁档案\n\n")
	if len(data.Ministers) == 0 {
		sb.WriteString("暂无部长。\n\n")
	}
	for _, ms := range data.Ministers {
		m := ms.Minister
		statusIcon := ministerIcon(m.Status)
		var skills []string
		if m.Skills != "" {
			_ = json.Unmarshal([]byte(m.Skills), &skills)
		}
		skillStr := strings.Join(skills, ", ")
		if skillStr == "" {
			skillStr = "通用"
		}
		rateStr := "-"
		if ms.Total > 0 {
			rate := float64(ms.Enacted) / float64(ms.Total) * 100
			rateStr = fmt.Sprintf("%d/%d (%.0f%%)", ms.Enacted, ms.Total, rate)
		}
		sb.WriteString(fmt.Sprintf("- %s **%s** [%s]  技能: %s  Hansard: %s\n",
			statusIcon, m.Title, m.Status, skillStr, rateStr))
	}
	sb.WriteString("\n")

	// Recent Gazettes.
	sb.WriteString("## 近期公报（最新 10 份）\n\n")
	if len(data.RecentGazettes) == 0 {
		sb.WriteString("暂无公报。\n\n")
	}
	for _, g := range data.RecentGazettes {
		from := g.FromMinister.String
		if from == "" {
			from = "system"
		}
		to := g.ToMinister.String
		if to == "" {
			to = "全体"
		}
		sb.WriteString(fmt.Sprintf("- **[%s]** %s → %s: %s\n",
			g.Type.String, from, to, truncate(g.Summary, 80)))
	}
	sb.WriteString("\n")

	sb.WriteString("---\n\n")
	sb.WriteString("*此备忘录由 Whip 自动生成，每 60 秒刷新。*\n")

	return sb.String()
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func billIcon(status string) string {
	switch status {
	case "draft":
		return "📝"
	case "reading":
		return "📖"
	case "committee":
		return "🔍"
	case "enacted":
		return "✅"
	case "royal_assent":
		return "👑"
	case "failed":
		return "❌"
	default:
		return "⚪"
	}
}

func ministerIcon(status string) string {
	switch status {
	case "working":
		return "🟢"
	case "idle":
		return "🟡"
	case "stuck":
		return "🔴"
	default:
		return "⚪"
	}
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}
