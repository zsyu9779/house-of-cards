package cmd

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/house-of-cards/hoc/internal/store"
	"github.com/spf13/cobra"
)

// ─── Styles ──────────────────────────────────────────────────────────────────

var (
	styleTitle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleSection   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	styleSubtle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleDivider   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleWorking   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	styleIdle      = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	styleStuck     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	styleEnacted   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	styleProgress  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue
	styleCommittee = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	styleOffline   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray
	styleKeyHint   = lipgloss.NewStyle().Foreground(lipgloss.Color("14")) // cyan
)

// ─── View Modes ──────────────────────────────────────────────────────────────

type viewMode int

const (
	viewMain viewMode = iota
	viewGazette
	viewSession
)

// ─── Model ───────────────────────────────────────────────────────────────────

type floorModel struct {
	db            *store.DB
	ministers     []*store.Minister
	sessions      []*store.Session
	bills         map[string][]*store.Bill // session_id → bills
	gazettes      []*store.Gazette         // all recent gazettes (not just unread)
	updatedAt     time.Time
	err           error
	width         int
	height        int
	quitting      bool
	mode          viewMode
	interval      time.Duration
	gazetteOffset int // scroll offset for gazette view
}

type tickMsg time.Time
type floorDataMsg struct {
	ministers []*store.Minister
	sessions  []*store.Session
	bills     map[string][]*store.Bill
	gazettes  []*store.Gazette
	updatedAt time.Time
}
type floorErrMsg error

func newFloorModel(d *store.DB, interval time.Duration) floorModel {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	return floorModel{db: d, width: 70, interval: interval}
}

func (m floorModel) Init() tea.Cmd {
	return tea.Batch(fetchFloorData(m.db), tickEvery(m.interval))
}

func fetchFloorData(d *store.DB) tea.Cmd {
	return func() tea.Msg {
		ministers, err := d.ListMinisters()
		if err != nil {
			return floorErrMsg(err)
		}
		sessions, err := d.ListSessions()
		if err != nil {
			return floorErrMsg(err)
		}
		// Fetch all recent gazettes (last 20) for the gazette view.
		gazettes, err := d.ListGazettes()
		if err != nil {
			return floorErrMsg(err)
		}
		// Limit to last 20.
		if len(gazettes) > 20 {
			gazettes = gazettes[:20]
		}
		billsMap := make(map[string][]*store.Bill)
		for _, s := range sessions {
			bs, _ := d.ListBillsBySession(s.ID)
			billsMap[s.ID] = bs
		}
		return floorDataMsg{
			ministers: ministers,
			sessions:  sessions,
			bills:     billsMap,
			gazettes:  gazettes,
			updatedAt: time.Now(),
		}
	}
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m floorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc", "b", "B":
			m.mode = viewMain
			m.gazetteOffset = 0
		case "r", "R":
			return m, fetchFloorData(m.db)
		case "g", "G":
			m.mode = viewGazette
			m.gazetteOffset = 0
		case "s", "S":
			if m.mode == viewSession {
				m.mode = viewMain
			} else {
				m.mode = viewSession
			}
		case "j", "down":
			if m.mode == viewGazette {
				m.gazetteOffset++
			}
		case "k", "up":
			if m.mode == viewGazette && m.gazetteOffset > 0 {
				m.gazetteOffset--
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.width < 50 {
			m.width = 50
		}

	case floorDataMsg:
		m.ministers = msg.ministers
		m.sessions = msg.sessions
		m.bills = msg.bills
		m.gazettes = msg.gazettes
		m.updatedAt = msg.updatedAt
		m.err = nil

	case floorErrMsg:
		m.err = msg

	case tickMsg:
		return m, tea.Batch(fetchFloorData(m.db), tickEvery(m.interval))
	}
	return m, nil
}

func (m floorModel) View() string {
	if m.quitting {
		return styleSubtle.Render("\n休会。(Adjourned)\n")
	}

	switch m.mode {
	case viewGazette:
		return m.viewGazetteList()
	case viewSession:
		return m.viewSessionDetail()
	case viewMain:
		return m.viewMain()
	default:
		return m.viewMain()
	}
}

// ─── Main View ───────────────────────────────────────────────────────────────

func (m floorModel) viewMain() string {
	w := m.width
	if w > 100 {
		w = 100
	}
	div := styleDivider.Render(strings.Repeat("═", w))
	thin := styleDivider.Render(strings.Repeat("─", w))

	var sb strings.Builder

	// ── Header ──────────────────────────────────────────────────
	sb.WriteString(div + "\n")
	sb.WriteString(styleTitle.Render("  🏛  House of Cards — 议会大厅 (Floor)") + "\n")
	updStr := "加载中..."
	if !m.updatedAt.IsZero() {
		updStr = m.updatedAt.Format("2006-01-02 15:04:05")
	}
	intervalStr := fmt.Sprintf("%.0fs", m.interval.Seconds())
	sb.WriteString(styleSubtle.Render(fmt.Sprintf("  %s  刷新:%s", updStr, intervalStr)))
	sb.WriteString("  " + styleKeyHint.Render("[r]刷新  [g]公报  [s]会期  [q]退出") + "\n")
	sb.WriteString(div + "\n")

	if m.err != nil {
		sb.WriteString(styleStuck.Render(fmt.Sprintf("\n  ❌ 错误: %v\n", m.err)))
		return sb.String()
	}

	// ── Cabinet ──────────────────────────────────────────────────
	working, idle, stuck, offline := 0, 0, 0, 0
	for _, mi := range m.ministers {
		switch mi.Status {
		case "working":
			working++
		case "idle":
			idle++
		case "stuck":
			stuck++
		case "offline":
			offline++
		}
	}
	sb.WriteString("\n")
	sb.WriteString(styleSection.Render(fmt.Sprintf("🏛  内阁 (%d 位)", len(m.ministers))))
	sb.WriteString("  ")
	sb.WriteString(styleWorking.Render(fmt.Sprintf("🟢工作:%d", working)))
	sb.WriteString("  ")
	sb.WriteString(styleIdle.Render(fmt.Sprintf("🟡待命:%d", idle)))
	sb.WriteString("  ")
	sb.WriteString(styleStuck.Render(fmt.Sprintf("🔴卡住:%d", stuck)))
	sb.WriteString("  ")
	sb.WriteString(styleOffline.Render(fmt.Sprintf("⚪离线:%d", offline)))
	sb.WriteString("\n" + thin + "\n")

	if len(m.ministers) == 0 {
		sb.WriteString(styleSubtle.Render("  (暂无部长)") + "\n")
	}
	now := time.Now()
	for _, mi := range m.ministers {
		icon := "⚪"
		nameStyle := styleOffline
		switch mi.Status {
		case "working":
			icon = "🟢"
			nameStyle = styleWorking
		case "idle":
			icon = "🟡"
			nameStyle = styleIdle
		case "stuck":
			icon = "🔴"
			nameStyle = styleStuck
		}

		hbStr := "—"
		if mi.Heartbeat.Valid {
			ago := now.Sub(mi.Heartbeat.Time).Round(time.Second)
			hbStr = fmtDuration(ago) + "前"
		}

		enacted, total, _ := m.db.HansardSuccessRate(mi.ID)
		rate := "—"
		if total > 0 {
			pct := int(float64(enacted) / float64(total) * 100)
			rate = fmt.Sprintf("%d/%d(%d%%)", enacted, total, pct)
		}

		line := fmt.Sprintf("  %s %-24s  ♡%-8s  📊%s",
			icon, truncate(mi.Title, 24), hbStr, rate)
		sb.WriteString(nameStyle.Render(line) + "\n")
	}

	// ── Sessions ─────────────────────────────────────────────────
	sb.WriteString("\n")
	sb.WriteString(styleSection.Render(fmt.Sprintf("📋 会期 (%d 个)", len(m.sessions))) + "\n")
	sb.WriteString(thin + "\n")

	if len(m.sessions) == 0 {
		sb.WriteString(styleSubtle.Render("  (暂无会期)") + "\n")
	}
	for _, s := range m.sessions {
		statusIcon := "🟢"
		lineStyle := styleWorking
		if s.Status == "completed" {
			statusIcon = "✅"
			lineStyle = styleEnacted
		} else if s.Status == "dissolved" {
			statusIcon = "⚫"
			lineStyle = styleSubtle
		}

		bills := m.bills[s.ID]
		counts := map[string]int{}
		for _, b := range bills {
			counts[b.Status]++
		}
		total := len(bills)
		enacted := counts["enacted"] + counts["royal_assent"]
		bar := buildProgressBar(enacted, total, 20)

		sb.WriteString(lineStyle.Render(fmt.Sprintf("  %s %s", statusIcon, truncate(s.Title, 46))) + "\n")
		sb.WriteString("     ")
		sb.WriteString(styleProgress.Render(bar))
		sb.WriteString(fmt.Sprintf("  %d/%d", enacted, total))
		if counts["committee"] > 0 {
			sb.WriteString(styleCommittee.Render(fmt.Sprintf("  🔍%d", counts["committee"])))
		}
		if counts["reading"] > 0 {
			sb.WriteString(styleSubtle.Render(fmt.Sprintf("  📖%d", counts["reading"])))
		}
		if counts["draft"] > 0 {
			sb.WriteString(styleSubtle.Render(fmt.Sprintf("  📝%d", counts["draft"])))
		}
		if counts["failed"] > 0 {
			sb.WriteString(styleStuck.Render(fmt.Sprintf("  ❌%d", counts["failed"])))
		}
		sb.WriteString("\n")
	}

	// ── Recent Gazettes (stream) ──────────────────────────────────
	sb.WriteString("\n")
	recentCount := len(m.gazettes)
	if recentCount > 5 {
		recentCount = 5
	}
	sb.WriteString(styleSection.Render(fmt.Sprintf("📰 最新公报 (最近 %d 份)", recentCount)) + "\n")
	sb.WriteString(thin + "\n")

	if len(m.gazettes) == 0 {
		sb.WriteString(styleSubtle.Render("  (暂无公报)") + "\n")
	}
	for _, g := range m.gazettes[:recentCount] {
		to := g.ToMinister.String
		if to == "" {
			to = "全体"
		}
		readMark := "·"
		if g.ReadAt.Valid {
			readMark = "✓"
		}
		line := fmt.Sprintf("  %s [%-10s] → %-15s  %s",
			readMark, padRight(g.Type.String, 10), to, truncate(g.Summary, 32))
		sb.WriteString(styleSubtle.Render(line) + "\n")
	}
	if len(m.gazettes) > 5 {
		sb.WriteString(styleSubtle.Render(fmt.Sprintf("  ... [g] 查看全部 %d 份公报\n", len(m.gazettes))))
	}

	sb.WriteString("\n" + div + "\n")
	sb.WriteString(styleSubtle.Render("  hoc whip report | hoc bill list | hoc gazette list") + "\n")

	return sb.String()
}

// ─── Gazette List View ───────────────────────────────────────────────────────

func (m floorModel) viewGazetteList() string {
	w := m.width
	if w > 100 {
		w = 100
	}
	div := styleDivider.Render(strings.Repeat("═", w))
	thin := styleDivider.Render(strings.Repeat("─", w))

	var sb strings.Builder
	sb.WriteString(div + "\n")
	sb.WriteString(styleTitle.Render("  📰 公报列表 (Gazette List)") + "\n")
	sb.WriteString(styleSubtle.Render("  [j/k]滚动  [b/esc]返回  [q]退出") + "\n")
	sb.WriteString(div + "\n\n")

	if len(m.gazettes) == 0 {
		sb.WriteString(styleSubtle.Render("  (暂无公报)") + "\n")
		return sb.String()
	}

	// Calculate visible window.
	pageSize := m.height - 8
	if pageSize < 5 {
		pageSize = 5
	}
	start := m.gazetteOffset
	if start >= len(m.gazettes) {
		start = len(m.gazettes) - 1
	}
	end := start + pageSize
	if end > len(m.gazettes) {
		end = len(m.gazettes)
	}

	sb.WriteString(styleSection.Render(fmt.Sprintf("共 %d 份公报 (%d-%d):", len(m.gazettes), start+1, end)) + "\n")
	sb.WriteString(thin + "\n")

	for _, g := range m.gazettes[start:end] {
		to := g.ToMinister.String
		if to == "" {
			to = "全体"
		}
		from := g.FromMinister.String
		if from == "" {
			from = "系统"
		}
		readMark := styleSubtle.Render("·")
		if g.ReadAt.Valid {
			readMark = styleWorking.Render("✓")
		}
		timeStr := g.CreatedAt.Format("01-02 15:04")
		sb.WriteString(fmt.Sprintf("  %s %s  %-10s  %s→%s\n",
			readMark,
			styleSubtle.Render(timeStr),
			styleIdle.Render("["+g.Type.String+"]"),
			styleSubtle.Render(truncate(from, 15)+" "),
			styleSubtle.Render(" "+truncate(to, 15)),
		))
		sb.WriteString(fmt.Sprintf("     %s\n", truncate(g.Summary, 70)))
		if g.BillID.String != "" {
			sb.WriteString(styleSubtle.Render(fmt.Sprintf("     议案: %s\n", g.BillID.String)))
		}
		sb.WriteString("\n")
	}

	if end < len(m.gazettes) {
		sb.WriteString(styleSubtle.Render(fmt.Sprintf("  [j]↓ 还有 %d 份", len(m.gazettes)-end)) + "\n")
	}

	return sb.String()
}

// ─── Session Detail View ─────────────────────────────────────────────────────

func (m floorModel) viewSessionDetail() string {
	w := m.width
	if w > 100 {
		w = 100
	}
	div := styleDivider.Render(strings.Repeat("═", w))
	thin := styleDivider.Render(strings.Repeat("─", w))

	var sb strings.Builder
	sb.WriteString(div + "\n")
	sb.WriteString(styleTitle.Render("  📋 会期详情 (Session Detail)") + "\n")
	sb.WriteString(styleSubtle.Render("  [s/b/esc]返回  [q]退出") + "\n")
	sb.WriteString(div + "\n\n")

	if len(m.sessions) == 0 {
		sb.WriteString(styleSubtle.Render("  (暂无会期)") + "\n")
		return sb.String()
	}

	for _, s := range m.sessions {
		statusIcon := "🟢"
		lineStyle := styleWorking
		if s.Status == "completed" {
			statusIcon = "✅"
			lineStyle = styleEnacted
		} else if s.Status == "dissolved" {
			statusIcon = "⚫"
			lineStyle = styleSubtle
		}

		bills := m.bills[s.ID]
		counts := map[string]int{}
		for _, b := range bills {
			counts[b.Status]++
		}
		total := len(bills)
		enacted := counts["enacted"] + counts["royal_assent"]
		bar := buildProgressBar(enacted, total, 24)

		sb.WriteString(lineStyle.Render(fmt.Sprintf("  %s %s  [%s]", statusIcon, s.Title, s.Topology)) + "\n")
		sb.WriteString(fmt.Sprintf("     ID: %s\n", styleSubtle.Render(s.ID)))
		sb.WriteString("     " + styleProgress.Render(bar) + fmt.Sprintf("  %d/%d 完成\n", enacted, total))

		if len(bills) > 0 {
			sb.WriteString(thin + "\n")
			for _, b := range bills {
				icon := billStatusIcon(b.Status)
				var statusStyle lipgloss.Style
				switch b.Status {
				case "working", "reading":
					statusStyle = styleWorking
				case "enacted", "royal_assent":
					statusStyle = styleEnacted
				case "committee":
					statusStyle = styleCommittee
				case "failed":
					statusStyle = styleStuck
				default:
					statusStyle = styleSubtle
				}
				assignee := b.Assignee.String
				if assignee == "" {
					assignee = "(未分配)"
				}
				line := fmt.Sprintf("     %s %-8s %-28s → %s",
					icon, b.Status, truncate(b.Title, 28), truncate(assignee, 20))
				sb.WriteString(statusStyle.Render(line) + "\n")
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// buildProgressBar builds a simple ASCII progress bar.
func buildProgressBar(done, total, width int) string {
	if total == 0 {
		return "[" + strings.Repeat("░", width) + "]"
	}
	filled := done * width / total
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func fmtDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

// ─── Command ─────────────────────────────────────────────────────────────────

var floorCmd = &cobra.Command{
	Use:   "floor",
	Short: "议会大厅 — 全局状态实时监控 (BubbleTea TUI)",
	Long: `议会大厅：实时展示所有 Minister、Session、Bill、Gazette 状态。

自动刷新（默认 3s）。键盘快捷键：
  [r]      立即刷新
  [g]      公报列表视图
  [s]      会期详情视图
  [b/esc]  返回主视图
  [q]      退出`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		intervalSec, _ := cmd.Flags().GetInt("interval")
		interval := time.Duration(intervalSec) * time.Second

		p := tea.NewProgram(newFloorModel(db, interval), tea.WithAltScreen())
		_, err := p.Run()
		return err
	},
}

//nolint:gochecknoinits // Cobra convention: register flags in init().
func init() {
	floorCmd.Flags().Int("interval", 3, "自动刷新间隔（秒）")
}
