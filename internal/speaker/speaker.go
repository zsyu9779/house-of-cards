// Package speaker implements the Speaker (议长) role: the AI orchestrator
// that receives high-level goals, decomposes them into Bills, and monitors
// overall session progress via a persistent context.
package speaker

import (
	"encoding/json"
	"fmt"
	"log/slog"
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
	ByElections    []*store.Hansard // recent by-election events
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

// Decision represents a single actionable directive from the Speaker.
type Decision struct {
	Action    string // "assign", "by-election", "escalate"
	Target    string // primary target (bill-id or minister-id)
	Secondary string // secondary arg (minister-id for "assign")
	Raw       string // original directive line
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

// SelectTopology analyzes a set of bills and recommends the best collaboration topology.
// Returns "parallel", "pipeline", or "tree".
func SelectTopology(bills []*store.Bill) string {
	if len(bills) == 0 {
		return "parallel"
	}

	// Count how many bills have at least one dependency.
	hasDeps := 0
	for _, b := range bills {
		if b.DependsOn.String != "" {
			hasDeps++
		}
	}

	// No dependencies at all → pure parallel.
	if hasDeps == 0 {
		return "parallel"
	}

	// Build a map: bill-id → how many other bills depend on it.
	dependentCount := make(map[string]int)
	for _, b := range bills {
		if b.DependsOn.String == "" {
			continue
		}
		var deps []string
		if err := json.Unmarshal([]byte(b.DependsOn.String), &deps); err != nil {
			// Treat malformed deps as a single dependency.
			deps = []string{b.DependsOn.String}
		}
		if len(deps) > 1 {
			// A bill with multiple upstream dependencies signals tree topology.
			return "tree"
		}
		for _, d := range deps {
			dependentCount[d]++
		}
	}

	// If any bill is depended upon by more than one other bill, it's a tree (fan-in).
	for _, count := range dependentCount {
		if count > 1 {
			return "tree"
		}
	}

	// All dependencies are single-in, single-out → linear pipeline.
	return "pipeline"
}

// ParseDecision parses a block of text and extracts Speaker directives.
// Directives must appear on their own lines in the format:
//
//	[DIRECTIVE] assign <bill-id> <minister-id>
//	[DIRECTIVE] by-election <minister-id>
//	[DIRECTIVE] escalate <bill-id>
func ParseDecision(text string) []Decision {
	var decisions []Decision
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[DIRECTIVE]") {
			continue
		}
		raw := line
		parts := strings.Fields(strings.TrimPrefix(line, "[DIRECTIVE]"))
		if len(parts) == 0 {
			continue
		}
		d := Decision{Raw: raw, Action: parts[0]}
		switch d.Action {
		case "assign":
			if len(parts) >= 3 {
				d.Target = parts[1]
				d.Secondary = parts[2]
				decisions = append(decisions, d)
			}
		case "by-election":
			if len(parts) >= 2 {
				d.Target = parts[1]
				decisions = append(decisions, d)
			}
		case "escalate":
			if len(parts) >= 2 {
				d.Target = parts[1]
				decisions = append(decisions, d)
			}
		}
	}
	return decisions
}

// RunPatrol invokes the Speaker AI with a structured patrol prompt (non-interactive)
// and returns the parsed directives from the AI's response.
func RunPatrol(hocDir, contextContent string) ([]Decision, error) {
	patrolPromptPath := filepath.Join(hocDir, ".hoc", "speaker", "patrol-prompt.md")
	if err := writePatrolPrompt(patrolPromptPath, contextContent); err != nil {
		return nil, fmt.Errorf("write patrol prompt: %w", err)
	}

	cmd := exec.Command("claude", "-p", "@"+patrolPromptPath)
	cmd.Dir = hocDir

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude patrol: %w", err)
	}

	return ParseDecision(string(out)), nil
}

// writePatrolPrompt writes the Speaker's patrol decision prompt.
func writePatrolPrompt(path, contextContent string) error {
	content := fmt.Sprintf(`# 议长巡视简报（Speaker Patrol Briefing）

> 你是 House of Cards 的议长（Speaker）。请基于以下政府状态快照做出决策。

## 当前政府状态

%s

## 决策规则

1. 如有 draft 状态且未分配的议案，且有合适的 idle 部长 → 输出 assign 指令
2. 如有 stuck 部长超过 10 分钟 → 输出 by-election 指令
3. 如同一议案 failed 超过 2 次 → 输出 escalate 指令
4. 无需行动时，不输出任何 [DIRECTIVE] 行，直接说明原因

## 指令输出格式（每条独占一行）

[DIRECTIVE] assign <bill-id> <minister-id>
[DIRECTIVE] by-election <minister-id>
[DIRECTIVE] escalate <bill-id>

## 你的决策（仅输出指令行和简短说明）：
`, contextContent)
	return os.WriteFile(path, []byte(content), 0644)
}

// SummonResult carries the outcome of a Speaker summon for the CLI caller.
// TmuxSession is the tmux session name when the speaker was started in
// detached mode; it is empty for interactive (foreground) runs.
type SummonResult struct {
	TmuxSession string
}

// Summon starts the Speaker AI session with the current context injected.
// useTmux=true runs in a detached tmux session named "hoc-speaker".
// useTmux=false runs interactively in the foreground.
//
// User-facing feedback (e.g. "speaker is ready") is NOT printed here — the
// caller (cmd layer) is responsible for rendering based on the returned
// SummonResult. This keeps internal packages free of fmt.Print calls.
func Summon(hocDir string, useTmux bool) (SummonResult, error) {
	ctxPath := ContextPath(hocDir)
	if _, err := os.Stat(ctxPath); os.IsNotExist(err) {
		return SummonResult{}, fmt.Errorf("Speaker context 文件不存在：%s\n请先运行 hoc speaker context --refresh", ctxPath)
	}

	// Read the context content to embed directly in the prompt.
	ctxBytes, err := os.ReadFile(ctxPath)
	if err != nil {
		return SummonResult{}, fmt.Errorf("read speaker context: %w", err)
	}
	contextContent := string(ctxBytes)

	speakerPromptPath := filepath.Join(hocDir, ".hoc", "speaker", "prompt.md")
	if err := writeSpeakerPrompt(speakerPromptPath, contextContent); err != nil {
		return SummonResult{}, fmt.Errorf("write speaker prompt: %w", err)
	}

	if useTmux {
		tmuxName := "hoc-speaker"
		if err := exec.Command("tmux", "kill-session", "-t", tmuxName).Run(); err != nil {
			// Non-fatal: session may simply not exist yet.
			slog.Debug("tmux kill-session (pre-summon)", "session", tmuxName, "err", err)
		}

		shellCmd := fmt.Sprintf("claude -p @%s", speakerPromptPath)
		cmd := exec.Command("tmux", "new-session", "-d",
			"-s", tmuxName,
			"-c", hocDir,
			shellCmd,
		)
		if output, err := cmd.CombinedOutput(); err != nil {
			return SummonResult{}, fmt.Errorf("tmux start: %w\n%s", err, string(output))
		}
		slog.Info("speaker summoned in tmux", "session", tmuxName)
		return SummonResult{TmuxSession: tmuxName}, nil
	}

	// Interactive foreground mode.
	cmd := exec.Command("claude", "-p", "@"+speakerPromptPath)
	cmd.Dir = hocDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return SummonResult{}, cmd.Run()
}

// writeSpeakerPrompt writes the speaker's initial system prompt with the
// current government context embedded directly (not via {{include:}} placeholder).
func writeSpeakerPrompt(path string, contextContent string) error {
	content := fmt.Sprintf(`# 议长就职简报（Speaker Briefing）

> 你是 House of Cards 多 Agent 协作框架的**议长（Speaker）**。
> 你的职责是：接收人类需求、拆解为 Bill（议案）、选择协作拓扑、监督 Session 进度、基于 Gazette 公报做决策。

---

## 政府当前状态

> 以下是最新的政府状态快照（生成时间：%s）：

%s

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
`, time.Now().Format("2006-01-02 15:04:05"), contextContent)
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

	// Fetch recent by-election records (max 3).
	byElections, _ := db.ListByElectionHansard(3)
	data.ByElections = byElections

	return data, nil
}

// ─── Context Rendering ────────────────────────────────────────────────────────

func renderContext(data *ContextData) string {
	var sb strings.Builder

	sb.WriteString("# Speaker Context（议长备忘录）\n\n")
	sb.WriteString(fmt.Sprintf("> 生成时间：%s\n\n", data.GeneratedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString("---\n\n")

	// Count minister status breakdowns (reused across sections).
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
	total := len(data.Ministers)

	// Government overview.
	sb.WriteString("## 政府现状\n\n")
	activeSessions := 0
	for _, s := range data.AllSessions {
		if s.Status == "active" {
			activeSessions++
		}
	}
	sb.WriteString(fmt.Sprintf("- 活跃会期：%d 个（共 %d 个）\n", activeSessions, len(data.AllSessions)))
	sb.WriteString(fmt.Sprintf("- 内阁：%d 位部长（工作中 %d / 待命 %d / 卡住 %d / 离线 %d）\n\n",
		total, working, idle, stuck, offline))

	// Resource utilization.
	sb.WriteString("## 资源利用率\n\n")
	active := working + idle
	utilization := 0.0
	if total > 0 {
		utilization = float64(active) / float64(total) * 100
	}
	sb.WriteString(fmt.Sprintf("- 在任部长（工作中+待命）：%d / %d (%.0f%%)\n", active, total, utilization))
	if stuck > 0 {
		sb.WriteString(fmt.Sprintf("- ⚠ 卡住部长 %d 位，需要关注\n", stuck))
	}
	sb.WriteString("\n")

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
		topology := ss.Session.Topology
		sb.WriteString(fmt.Sprintf("### %s %s [%s] (%d/%d Bills) | 拓扑: `%s`\n\n",
			icon, ss.Session.Title, ss.Session.Status, ss.Done, ss.Total, topology))

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

	// Topology recommendations for active sessions.
	sb.WriteString("## 拓扑推荐\n\n")
	hasActive := false
	for _, ss := range data.ActiveSessions {
		if ss.Session.Status != "active" {
			continue
		}
		hasActive = true
		recommended := SelectTopology(ss.Bills)
		current := ss.Session.Topology
		match := "✓ 当前拓扑合适"
		if recommended != current {
			match = fmt.Sprintf("⚡ 建议改为 `%s`", recommended)
		}
		sb.WriteString(fmt.Sprintf("- **%s**: 当前 `%s` — %s（%s）\n",
			ss.Session.Title, current, topologyReason(current), match))
	}
	if !hasActive {
		sb.WriteString("暂无活跃会期。\n")
	}
	sb.WriteString("\n")

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

	// Recent by-election records.
	if len(data.ByElections) > 0 {
		sb.WriteString("## 近期补选记录\n\n")
		for _, be := range data.ByElections {
			note := be.Notes.String
			if note == "" {
				note = "原因不明"
			}
			sb.WriteString(fmt.Sprintf("- 部长 `%s`，议案 `%s`：%s (%s)\n",
				be.MinisterID, be.BillID, truncate(note, 80), be.CreatedAt.Format("01/02 15:04")))
		}
		sb.WriteString("\n")
	}

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

	// Recommended actions.
	sb.WriteString("## 推荐行动\n\n")
	actions := generateRecommendedActions(data, working, idle, stuck)
	for i, action := range actions {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, action))
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

// topologyReason returns a brief human-readable explanation of why a topology was chosen.
func topologyReason(topology string) string {
	switch topology {
	case "parallel":
		return "各议案独立并行，适合前后端分工"
	case "pipeline":
		return "逐步依赖传递，适合数据管道"
	case "tree":
		return "多议案汇合，适合集成前的并行开发"
	case "mesh":
		return "多向依赖，适合架构设计讨论"
	default:
		return "自定义拓扑"
	}
}

// generateRecommendedActions produces a prioritized list of suggested actions
// based on the current government state.
func generateRecommendedActions(data *ContextData, working, idle, stuck int) []string {
	var actions []string

	// Priority 1: stuck ministers need immediate attention.
	for _, ms := range data.Ministers {
		if ms.Minister.Status == "stuck" {
			actions = append(actions, fmt.Sprintf("🔴 [紧急] 部长 [%s] 卡住，建议触发补选（by-election）", ms.Minister.ID))
		}
	}

	// Priority 2: unassigned bills in active sessions.
	draftCount := 0
	for _, ss := range data.ActiveSessions {
		if ss.Session.Status != "active" {
			continue
		}
		for _, b := range ss.Bills {
			if b.Status == "draft" && b.Assignee.String == "" {
				draftCount++
				if draftCount <= 3 {
					actions = append(actions, fmt.Sprintf("📝 议案 [%s] \"%s\" 等待分配（draft）",
						b.ID, truncate(b.Title, 40)))
				}
			}
		}
	}
	if draftCount > 3 {
		actions = append(actions, fmt.Sprintf("📝 还有 %d 份草案等待分配", draftCount-3))
	}

	// Priority 3: idle ministers with nothing to do.
	if idle > 0 && draftCount == 0 {
		actions = append(actions, fmt.Sprintf("💡 %d 位部长待命但无议案，可考虑开启新会期", idle))
	}

	// Priority 4: all good.
	if len(actions) == 0 {
		actions = append(actions, "✅ 系统运行正常，无紧急行动")
		if working > 0 {
			actions = append(actions, fmt.Sprintf("📊 %d 位部长正在工作，等待 done 文件信号", working))
		}
	}

	return actions
}
