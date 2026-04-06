package cmd

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/house-of-cards/hoc/internal/store"
	"github.com/house-of-cards/hoc/internal/util"
	"github.com/spf13/cobra"
)

// sessionSpec mirrors the TOML format for session files.
// Example:
//
//	[session]
//	title    = "Build Auth System"
//	topology = "parallel"
//
//	[[bills]]
//	id          = "auth-api"
//	title       = "Build JWT API"
//	motion      = "Implement JWT auth endpoints in Go"
//	portfolio   = "go"
//	depends_on  = []
type sessionSpec struct {
	Session sessionMeta `toml:"session"`
	Bills   []billSpec  `toml:"bills"`
}

type sessionMeta struct {
	Title    string `toml:"title"`
	Topology string `toml:"topology"`
	Project  string `toml:"project"`  // Legacy: single project
	Projects string `toml:"projects"` // Multi-project: comma-separated list
}

type billSpec struct {
	ID        string   `toml:"id"`
	Title     string   `toml:"title"`
	Motion    string   `toml:"motion"`
	Portfolio string   `toml:"portfolio"`
	Project   string   `toml:"project"` // Optional: specific project for this bill
	DependsOn []string `toml:"depends_on"`
}

// sessionCmd represents the session command.
var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "管理 Session（会期）",
	Long:  "会期管理命令：开启、状态、解散",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

//nolint:gochecknoinits // Cobra convention: register subcommands in init().
func init() {
	sessionCmd.AddCommand(sessionOpenCmd)
	sessionCmd.AddCommand(sessionStatusCmd)
	sessionCmd.AddCommand(sessionDissolveCmd)
	sessionCmd.AddCommand(sessionMigrateCmd) // Phase 3E
	sessionCmd.AddCommand(sessionStatsCmd)   // Phase 5A
	sessionCmd.AddCommand(sessionPauseCmd)   // Phase 3: D-3
	sessionCmd.AddCommand(sessionResumeCmd)  // Phase 3: D-3
	sessionCmd.AddCommand(sessionAdvanceCmd) // Phase 3: D-3
	sessionCmd.AddCommand(sessionReplayCmd)  // Phase 4: C-3

	sessionOpenCmd.Flags().String("project", "", "单个项目名称（已废弃，使用 --projects）")
	sessionOpenCmd.Flags().String("projects", "", "项目列表，逗号分隔（用于多项目会期）")
	sessionOpenCmd.Flags().Bool("force", false, "跳过议案标题校验")
	sessionStatusCmd.Flags().Bool("json", false, "以 JSON 格式输出")

	sessionMigrateCmd.Flags().String("project", "", "迁移时使用的默认项目名称")
	sessionMigrateCmd.Flags().Bool("confirm", false, "执行迁移（默认为预演模式）")

	sessionStatsCmd.Flags().Bool("all", false, "显示所有会期的汇总统计")

	sessionPauseCmd.Flags().String("reason", "", "暂停原因")
	sessionAdvanceCmd.Flags().Bool("force", false, "强制推进（确认所有 draft bill 可调度）")
}

var sessionOpenCmd = &cobra.Command{
	Use:   "open [session-file.toml]",
	Short: "开启新会期（从 TOML 文件）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		// Resolve file path.
		specPath := args[0]
		if !filepath.IsAbs(specPath) {
			cwd, _ := os.Getwd()
			specPath = filepath.Join(cwd, specPath)
		}

		// Parse the session TOML.
		var spec sessionSpec
		if _, err := toml.DecodeFile(specPath, &spec); err != nil {
			return fmt.Errorf("parse session file: %w", err)
		}
		if spec.Session.Title == "" {
			return fmt.Errorf("session.title is required in %s", specPath)
		}
		if spec.Session.Topology == "" {
			spec.Session.Topology = "parallel"
		}

		// --projects flag (comma-separated) overrides TOML project field.
		projectsFlag, _ := cmd.Flags().GetString("projects")
		projectFlag, _ := cmd.Flags().GetString("project")

		// Build projects JSON array.
		var projectsJSON string
		if projectsFlag != "" {
			// Split by comma and build JSON array.
			projects := strings.Split(projectsFlag, ",")
			for i := range projects {
				projects[i] = strings.TrimSpace(projects[i])
			}
			b, _ := json.Marshal(projects)
			projectsJSON = string(b)
		} else if projectFlag != "" {
			// Legacy single project support.
			projectsJSON = `["` + projectFlag + `"]`
		}

		// D-2: Input Guard - validate bill titles
		force, _ := cmd.Flags().GetBool("force")
		if !force {
			for _, bs := range spec.Bills {
				if err := store.ValidateBillTitle(bs.Title); err != nil {
					return fmt.Errorf("议案 [%s] 标题校验失败: %w\n使用 --force 跳过校验", bs.ID, err)
				}
			}
		}

		// Generate session ID.
		sid := shortID("session")

		// Create session in DB.
		sess := &store.Session{
			ID:       sid,
			Title:    spec.Session.Title,
			Topology: spec.Session.Topology,
			Project:  store.NullString(projectFlag),
			Projects: store.NullString(projectsJSON),
			Status:   "active",
		}
		if err := db.CreateSession(sess); err != nil {
			return fmt.Errorf("create session: %w", err)
		}

		fmt.Printf("✅ 会期已开启\n")
		fmt.Printf("   ID:       %s\n", sid)
		fmt.Printf("   标题:     %s\n", spec.Session.Title)
		fmt.Printf("   拓扑:     %s\n", spec.Session.Topology)
		if projectsJSON != "" && projectsJSON != "[]" {
			projects := sess.GetProjectsSlice()
			fmt.Printf("   项目:     %v\n", projects)
		}
		fmt.Printf("   议案数:   %d\n\n", len(spec.Bills))

		// Derive a short prefix from the session ID hex (after "session-").
		sessionHex := sid[len("session-"):]

		// First pass: compute all full bill IDs (with session hex prefix).
		billIDMap := make(map[string]string) // raw TOML id → full db id
		for _, bs := range spec.Bills {
			if bs.ID != "" {
				billIDMap[bs.ID] = fmt.Sprintf("%s-%s", sessionHex, bs.ID)
			}
		}

		// Second pass: create each bill with resolved dependency IDs and portfolio.
		for _, bs := range spec.Bills {
			billID := bs.ID
			if billID == "" {
				billID = shortID("bill")
			} else {
				billID = billIDMap[bs.ID]
			}

			// Resolve depends_on to full IDs.
			resolvedDeps := make([]string, 0, len(bs.DependsOn))
			for _, dep := range bs.DependsOn {
				if full, ok := billIDMap[dep]; ok {
					resolvedDeps = append(resolvedDeps, full)
				} else {
					resolvedDeps = append(resolvedDeps, dep)
				}
			}
			dependsJSON := "[]"
			if len(resolvedDeps) > 0 {
				b, _ := json.Marshal(resolvedDeps)
				dependsJSON = string(b)
			}

			bill := &store.Bill{
				ID:          billID,
				SessionID:   store.NullString(sid),
				Title:       bs.Title,
				Description: store.NullString(bs.Motion),
				Status:      "draft",
				DependsOn:   store.NullString(dependsJSON),
				Portfolio:   store.NullString(bs.Portfolio),
				Project:     store.NullString(bs.Project),
			}
			if err := db.CreateBill(bill); err != nil {
				return fmt.Errorf("create bill %s: %w", bs.ID, err)
			}
			_ = db.RecordEvent("bill.created", "cli", billID, "", sid, "")
			notes := ""
			if bs.Portfolio != "" {
				notes = fmt.Sprintf(" [portfolio: %s]", bs.Portfolio)
			}
			if bs.Project != "" {
				notes += fmt.Sprintf(" [project: %s]", bs.Project)
			}
			fmt.Printf("   📄 [%s] %s%s\n", billID, bs.Title, notes)
		}

		fmt.Printf("\n使用 `hoc bill list` 查看议案，`hoc bill assign <id> <minister>` 分配议案。\n")
		return nil
	},
}

var sessionStatusCmd = &cobra.Command{
	Use:   "status [session-id]",
	Short: "查看会期状态（可附带 ID 显示 DAG）",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		jsonMode, _ := cmd.Flags().GetBool("json")

		// Single session mode — show detailed view with DAG.
		if len(args) == 1 {
			return showSessionDetail(args[0], jsonMode)
		}

		// List all sessions mode.
		sessions, err := db.ListSessions()
		if err != nil {
			return fmt.Errorf("list sessions: %w", err)
		}

		if jsonMode {
			return encodeSessionsJSON(cmd, sessions)
		}

		if len(sessions) == 0 {
			fmt.Println("暂无会期。使用 `hoc session open <file.toml>` 开启会期。")
			return nil
		}

		for _, s := range sessions {
			statusIcon := "🟢"
			if s.Status == "completed" {
				statusIcon = "✅"
			} else if s.Status == "dissolved" {
				statusIcon = "⚫"
			} else if s.Status == "paused" {
				statusIcon = "⏸"
			}
			projStr := ""
			if s.Project.String != "" {
				projStr = fmt.Sprintf("  项目: %s", s.Project.String)
			}
			fmt.Printf("%s [%s] %s  (拓扑: %s%s)\n", statusIcon, s.Status, s.Title, s.Topology, projStr)
			fmt.Printf("   ID: %s  |  开启: %s\n", s.ID, s.CreatedAt.Format(time.RFC3339))

			bills, err := db.ListBillsBySession(s.ID)
			if err != nil {
				continue
			}
			for _, b := range bills {
				icon := billStatusIcon(b.Status)
				assignee := b.Assignee.String
				if assignee == "" {
					assignee = "(未分配)"
				}
				fmt.Printf("     %s [%s] %s → %s\n", icon, b.Status, b.Title, assignee)
			}
			fmt.Println()
		}
		return nil
	},
}

// showSessionDetail shows a single session's full status including ASCII DAG.
func showSessionDetail(sessionID string, jsonMode bool) error {
	s, err := db.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	bills, err := db.ListBillsBySession(sessionID)
	if err != nil {
		return fmt.Errorf("list bills: %w", err)
	}

	if jsonMode {
		return encodeSessionsJSON(nil, []*store.Session{s})
	}

	statusIcon := "🟢"
	if s.Status == "completed" {
		statusIcon = "✅"
	} else if s.Status == "dissolved" {
		statusIcon = "⚫"
	}
	projStr := ""
	if s.Project.String != "" {
		projStr = fmt.Sprintf("\n   项目:     %s", s.Project.String)
	}

	fmt.Printf("%s [%s] %s\n", statusIcon, s.Status, s.Title)
	fmt.Printf("   ID:       %s\n", s.ID)
	fmt.Printf("   拓扑:     %s%s\n", s.Topology, projStr)
	fmt.Printf("   开启:     %s\n", s.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("   议案数:   %d\n", len(bills))
	fmt.Println()

	if len(bills) == 0 {
		fmt.Println("本会期暂无议案。")
		return nil
	}

	// Bill summary table.
	fmt.Println("议案列表:")
	fmt.Println("  ─────────────────────────────────────────────────────")
	for _, b := range bills {
		icon := billStatusIcon(b.Status)
		assignee := b.Assignee.String
		if assignee == "" {
			assignee = "(未分配)"
		}
		fmt.Printf("  %s %-8s  %-30s  → %s\n", icon, b.Status, truncate(b.Title, 30), assignee)
	}
	fmt.Println()

	// ASCII DAG.
	fmt.Println("依赖关系图 (DAG):")
	fmt.Println("  ─────────────────────────────────────────────────────")

	// Convert bills to DAGItems.
	dagItems := make([]*util.DAGItem, 0, len(bills))
	for _, b := range bills {
		dagItems = append(dagItems, &util.DAGItem{
			ID:        b.ID,
			Title:     b.Title,
			Status:    b.Status,
			DependsOn: util.ParseDepsJSON(b.DependsOn.String),
		})
	}

	roots := util.BuildDAG(dagItems)
	dag := util.RenderDAG(roots)
	// Indent each line by 2 spaces.
	for _, line := range splitLines(dag) {
		fmt.Printf("  %s\n", line)
	}
	return nil
}

// encodeSessionsJSON encodes sessions as JSON (used by --json flag).
func encodeSessionsJSON(cmd *cobra.Command, sessions []*store.Session) error {
	type billSummary struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Status   string `json:"status"`
		Assignee string `json:"assignee"`
		Branch   string `json:"branch"`
	}
	type sessionJSON struct {
		ID        string        `json:"id"`
		Title     string        `json:"title"`
		Status    string        `json:"status"`
		Topology  string        `json:"topology"`
		Project   string        `json:"project,omitempty"`
		CreatedAt string        `json:"created_at"`
		Bills     []billSummary `json:"bills"`
	}
	out := make([]sessionJSON, 0, len(sessions))
	for _, s := range sessions {
		bills, _ := db.ListBillsBySession(s.ID)
		bSummaries := make([]billSummary, 0, len(bills))
		for _, b := range bills {
			bSummaries = append(bSummaries, billSummary{
				ID:       b.ID,
				Title:    b.Title,
				Status:   b.Status,
				Assignee: b.Assignee.String,
				Branch:   b.Branch.String,
			})
		}
		out = append(out, sessionJSON{
			ID:        s.ID,
			Title:     s.Title,
			Status:    s.Status,
			Topology:  s.Topology,
			Project:   s.Project.String,
			CreatedAt: s.CreatedAt.Format(time.RFC3339),
			Bills:     bSummaries,
		})
	}
	var w interface{ Write([]byte) (int, error) }
	if cmd != nil {
		w = cmd.OutOrStdout()
	} else {
		w = os.Stdout
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// splitLines splits a string into lines, omitting a trailing empty line.
func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

var sessionDissolveCmd = &cobra.Command{
	Use:   "dissolve [session-id]",
	Short: "解散会期",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		sid := args[0]
		s, err := db.GetSession(sid)
		if err != nil {
			return fmt.Errorf("session not found: %s", sid)
		}

		if err := db.UpdateSessionStatus(sid, "dissolved"); err != nil {
			return fmt.Errorf("update session: %w", err)
		}

		fmt.Printf("✓ 会期 [%s] %s 已解散\n", sid, s.Title)
		return nil
	},
}

func billStatusIcon(status string) string {
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
	case "epic":
		return "📦"
	default:
		return "⚪"
	}
}

// shortID generates a short random hex ID with a prefix.
func shortID(prefix string) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based.
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%x", prefix, b)
}

// ─── Phase 5A: session stats ──────────────────────────────────────────────────

var sessionStatsCmd = &cobra.Command{
	Use:   "stats [session-id]",
	Short: "显示会期统计数据（议案状态、质量、耗时、部长负荷）",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		allMode, _ := cmd.Flags().GetBool("all")

		if allMode {
			return showAllSessionStats()
		}
		if len(args) == 1 {
			return showSessionStats(args[0])
		}

		// Default: show stats for all active sessions.
		return showAllSessionStats()
	},
}

func showSessionStats(sessionID string) error {
	stats, err := db.GetSessionStats(sessionID)
	if err != nil {
		return fmt.Errorf("get session stats: %w", err)
	}
	printSessionStats(stats)
	return nil
}

func showAllSessionStats() error {
	allStats, err := db.GetAllSessionStats()
	if err != nil {
		return fmt.Errorf("get all session stats: %w", err)
	}
	if len(allStats) == 0 {
		fmt.Println("暂无会期数据。使用 `hoc session open` 开启会期。")
		return nil
	}
	for _, stats := range allStats {
		printSessionStats(stats)
		fmt.Println()
	}
	return nil
}

func printSessionStats(stats *store.SessionStats) {
	statusIcon := "🟢"
	if stats.Status == "completed" {
		statusIcon = "✅"
	} else if stats.Status == "dissolved" {
		statusIcon = "⚫"
	}

	fmt.Printf("📊 会期统计 — \"%s\" [%s]\n", stats.Title, stats.SessionID)
	fmt.Printf("   拓扑: %s  |  状态: %s %s\n", stats.Topology, statusIcon, stats.Status)
	fmt.Println("──────────────────────────────────────────────────")

	if stats.TotalBills == 0 {
		fmt.Println("  本会期暂无议案。")
		return
	}

	fmt.Printf("  议案总数: %d\n", stats.TotalBills)

	// Status breakdown.
	enacted := stats.ByStatus["enacted"] + stats.ByStatus["royal_assent"]
	inProgress := stats.ByStatus["reading"] + stats.ByStatus["committee"]
	failed := stats.ByStatus["failed"]
	draft := stats.ByStatus["draft"]

	pct := 0
	if stats.TotalBills > 0 {
		pct = int(stats.EnactedRate * 100)
	}
	fmt.Printf("  ✅ 已通过: %d (%d%%)   📖 进行中: %d   ❌ 失败: %d   📝 草案: %d\n",
		enacted, pct, inProgress, failed, draft)

	// Quality bar.
	if stats.AvgQuality > 0 {
		bar := buildQualityBar(stats.AvgQuality, 20)
		fmt.Printf("  平均质量:  %s  %.2f\n", bar, stats.AvgQuality)
	} else {
		fmt.Printf("  平均质量:  —（暂无 Hansard 数据）\n")
	}

	// Duration.
	if stats.TotalDurS > 0 {
		fmt.Printf("  总耗时:    %s\n", formatDuration(stats.TotalDurS))
	}

	// Per-minister breakdown.
	if len(stats.Ministers) > 0 {
		fmt.Println()
		fmt.Println("  部长表现:")
		for _, ml := range stats.Ministers {
			qualStr := "—"
			if ml.AvgQ > 0 {
				qualStr = fmt.Sprintf("%.2f", ml.AvgQ)
			}
			bar := buildQualityBar(ml.AvgQ, 10)
			fmt.Printf("  %-22s %s  %d bill(s)  %d✅  质量%s\n",
				truncate(ml.Title, 22), bar, ml.Bills, ml.Enacted, qualStr)
		}
	}
}

// ─── Phase 3: D-3 Session Governance Commands ────────────────────────────────

var sessionPauseCmd = &cobra.Command{
	Use:   "pause [session-id]",
	Short: "暂停会期（active → paused）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		sid := args[0]
		reason, _ := cmd.Flags().GetString("reason")

		s, err := db.GetSession(sid)
		if err != nil {
			return fmt.Errorf("session not found: %s", sid)
		}

		if s.Status != "active" {
			return fmt.Errorf("会期状态为 [%s]，只有 active 状态的会期可暂停", s.Status)
		}

		if err := db.UpdateSessionStatus(sid, "paused"); err != nil {
			return fmt.Errorf("update session status: %w", err)
		}

		payload := fmt.Sprintf(`{"reason":"%s"}`, reason)
		_ = db.RecordEvent("governance.session_paused", "cli", "", "", sid, payload)

		fmt.Printf("⏸  会期 [%s] \"%s\" 已暂停\n", sid, s.Title)
		if reason != "" {
			fmt.Printf("   原因: %s\n", reason)
		}
		fmt.Printf("   使用 `hoc session resume %s` 恢复。\n", sid)
		return nil
	},
}

var sessionResumeCmd = &cobra.Command{
	Use:   "resume [session-id]",
	Short: "恢复已暂停的会期（paused → active）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		sid := args[0]
		s, err := db.GetSession(sid)
		if err != nil {
			return fmt.Errorf("session not found: %s", sid)
		}

		if s.Status != "paused" {
			return fmt.Errorf("会期状态为 [%s]，只有 paused 状态的会期可恢复", s.Status)
		}

		if err := db.UpdateSessionStatus(sid, "active"); err != nil {
			return fmt.Errorf("update session status: %w", err)
		}

		_ = db.RecordEvent("governance.session_resumed", "cli", "", "", sid, "")

		fmt.Printf("▶  会期 [%s] \"%s\" 已恢复（paused → active）\n", sid, s.Title)
		return nil
	},
}

var sessionAdvanceCmd = &cobra.Command{
	Use:   "advance [session-id]",
	Short: "强制推进会期（确认所有 draft bill 可调度）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		sid := args[0]
		force, _ := cmd.Flags().GetBool("force")

		s, err := db.GetSession(sid)
		if err != nil {
			return fmt.Errorf("session not found: %s", sid)
		}

		if s.Status != "active" && s.Status != "paused" {
			return fmt.Errorf("会期状态为 [%s]，不可推进", s.Status)
		}

		if !force {
			return fmt.Errorf("请使用 --force 确认推进操作")
		}

		// Ensure session is active.
		if s.Status == "paused" {
			if err := db.UpdateSessionStatus(sid, "active"); err != nil {
				return fmt.Errorf("update session status: %w", err)
			}
		}

		_ = db.RecordEvent("governance.session_advanced", "cli", "", "", sid, "")

		// List draft bills for visibility.
		bills, err := db.ListBillsBySession(sid)
		if err != nil {
			return fmt.Errorf("list bills: %w", err)
		}

		draftCount := 0
		for _, b := range bills {
			if b.Status == "draft" {
				draftCount++
			}
		}

		fmt.Printf("⏩ 会期 [%s] \"%s\" 已强制推进\n", sid, s.Title)
		fmt.Printf("   状态: %s → active  待调度 draft 议案: %d\n", s.Status, draftCount)
		return nil
	},
}

// ─── Phase 4: C-3 Session Replay ──────────────────────────────────────────────

var sessionReplayCmd = &cobra.Command{
	Use:   "replay [session-id]",
	Short: "回放会期时间线（事件 + 议事录）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		sid := args[0]
		s, err := db.GetSession(sid)
		if err != nil {
			return fmt.Errorf("session not found: %s", sid)
		}

		events, err := db.ListEventsBySession(sid)
		if err != nil {
			return fmt.Errorf("list events: %w", err)
		}

		hansards, err := db.ListHansardBySession(sid)
		if err != nil {
			return fmt.Errorf("list hansard: %w", err)
		}

		timeline := buildReplayTimeline(events, hansards)

		fmt.Printf("📽  会期回放 — \"%s\" [%s]\n", s.Title, s.ID)
		fmt.Println("═══════════════════════════════════════════════")

		if len(timeline) == 0 {
			fmt.Println("  （无事件记录）")
		}
		for _, entry := range timeline {
			fmt.Println(entry)
		}

		// Session stats summary.
		bills, _ := db.ListBillsBySession(sid)
		totalBills := len(bills)
		enacted := 0
		failed := 0
		var totalQuality float64
		qualityCount := 0
		byElections := 0

		for _, b := range bills {
			switch b.Status {
			case "enacted", "royal_assent":
				enacted++
			case "failed":
				failed++
			}
		}
		for _, h := range hansards {
			if h.Quality > 0 {
				totalQuality += h.Quality
				qualityCount++
			}
		}
		for _, e := range events {
			if strings.HasPrefix(e.Topic, "by_election") {
				byElections++
			}
		}

		avgQ := 0.0
		if qualityCount > 0 {
			avgQ = totalQuality / float64(qualityCount)
		}

		fmt.Println()
		fmt.Println("── 会期统计 ──")
		fmt.Printf("  总议案: %d  通过: %d  失败: %d  平均质量: %.2f\n", totalBills, enacted, failed, avgQ)
		fmt.Printf("  补选次数: %d\n", byElections)

		// Per-minister performance.
		type ministerPerf struct {
			enacted int
			total   int
			totalQ  float64
			qCount  int
		}
		perfMap := make(map[string]*ministerPerf)
		for _, h := range hansards {
			mp, ok := perfMap[h.MinisterID]
			if !ok {
				mp = &ministerPerf{}
				perfMap[h.MinisterID] = mp
			}
			mp.total++
			if h.Outcome.String == "enacted" {
				mp.enacted++
			}
			if h.Quality > 0 {
				mp.totalQ += h.Quality
				mp.qCount++
			}
		}

		if len(perfMap) > 0 {
			fmt.Println()
			fmt.Println("── 部长表现 ──")
			for mid, mp := range perfMap {
				avgMQ := 0.0
				if mp.qCount > 0 {
					avgMQ = mp.totalQ / float64(mp.qCount)
				}
				bar := buildQualityBar(avgMQ, 10)
				fmt.Printf("  %-20s %s %.2f  %d/%d\n", truncate(mid, 20), bar, avgMQ, mp.enacted, mp.total)
			}
		}

		return nil
	},
}

// timelineEntry represents a single entry in the replay timeline.
type timelineEntry struct {
	ts   time.Time
	line string
}

// buildReplayTimeline merges events and hansard records into a chronological timeline.
func buildReplayTimeline(events []*store.Event, hansards []*store.Hansard) []string {
	var entries []timelineEntry

	for _, e := range events {
		icon := topicIcon(e.Topic)
		detail := e.BillID.String
		if e.MinisterID.String != "" {
			if detail != "" {
				detail += " → " + e.MinisterID.String
			} else {
				detail = e.MinisterID.String
			}
		}
		line := fmt.Sprintf("[%s] %s %-22s %s",
			e.Timestamp.Format("2006-01-02 15:04:05"),
			icon,
			e.Topic,
			detail,
		)
		entries = append(entries, timelineEntry{ts: e.Timestamp, line: line})
	}

	for _, h := range hansards {
		icon := "📜"
		if h.Outcome.String == "enacted" {
			icon = "✅"
		} else if h.Outcome.String == "failed" {
			icon = "❌"
		}
		qualStr := ""
		if h.Quality > 0 {
			qualStr = fmt.Sprintf("  质量: %.2f", h.Quality)
		}
		durStr := ""
		if h.DurationS > 0 {
			durStr = fmt.Sprintf("  耗时: %s", formatDuration(h.DurationS))
		}
		line := fmt.Sprintf("[%s] %s hansard.%s         %s → %s%s%s",
			h.CreatedAt.Format("2006-01-02 15:04:05"),
			icon,
			h.Outcome.String,
			h.MinisterID,
			h.BillID,
			qualStr,
			durStr,
		)
		entries = append(entries, timelineEntry{ts: h.CreatedAt, line: line})
	}

	// Sort by timestamp.
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].ts.Before(entries[i].ts) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = e.line
	}
	return lines
}

// topicIcon maps event topics to display icons.
func topicIcon(topic string) string {
	switch {
	case topic == "bill.created":
		return "📄"
	case topic == "bill.assigned":
		return "🔄"
	case topic == "bill.enacted":
		return "✅"
	case topic == "minister.stuck":
		return "🔴"
	case strings.HasPrefix(topic, "by_election"):
		return "🗳"
	case strings.HasPrefix(topic, "gazette"):
		return "📨"
	case topic == "session.completed":
		return "🏁"
	case strings.HasPrefix(topic, "privy"):
		return "⚖️"
	case strings.HasPrefix(topic, "committee"):
		return "🔍"
	case strings.HasPrefix(topic, "governance"):
		return "🏛"
	default:
		return "📌"
	}
}

// ─── Phase 3E: session migrate ────────────────────────────────────────────────

// sessionMigrateCmd migrates legacy sessions (no project field) to multi-project format.
// Also resolves the privyAutoMerge issue where whip couldn't merge old sessions.
var sessionMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "迁移旧会期：补全 project 字段（Phase 3E）",
	Long: `检测并修复旧会期（缺少 project 字段）。

旧会期无法触发枢密院自动合并（privyAutoMerge），通过此命令补全。

示例：
  hoc session migrate --project myapp              # 预演
  hoc session migrate --project myapp --confirm    # 执行迁移`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		defaultProject, _ := cmd.Flags().GetString("project")
		confirm, _ := cmd.Flags().GetBool("confirm")

		sessions, err := db.ListSessions()
		if err != nil {
			return fmt.Errorf("list sessions: %w", err)
		}

		var needsMigration []*store.Session
		for _, s := range sessions {
			if len(s.GetProjectsSlice()) == 0 {
				needsMigration = append(needsMigration, s)
			}
		}

		if len(needsMigration) == 0 {
			fmt.Println("✅ 所有会期均已有 project 字段，无需迁移。")
			return nil
		}

		fmt.Printf("📋 需要迁移的会期: %d 个\n\n", len(needsMigration))
		for _, s := range needsMigration {
			targetProject := defaultProject
			fmt.Printf("  [%s] %s — 当前 project: %q → 迁移为: %q\n",
				s.ID, s.Title,
				s.Project.String,
				targetProject,
			)
		}
		fmt.Println()

		if !confirm {
			fmt.Println("ℹ  预演模式。使用 --confirm 执行实际迁移。")
			if defaultProject == "" {
				fmt.Println("  ⚠  请指定 --project <name> 作为默认项目名。")
			}
			return nil
		}

		if defaultProject == "" {
			return fmt.Errorf("执行迁移时必须指定 --project <name>")
		}

		migrated := 0
		for _, s := range needsMigration {
			projectJSON := `["` + defaultProject + `"]`
			if err := db.UpdateSessionProjects(s.ID, projectJSON); err != nil {
				fmt.Printf("  ❌ 迁移失败 [%s]: %v\n", s.ID, err)
				continue
			}
			if err := db.UpdateSessionProject(s.ID, defaultProject); err != nil {
				fmt.Printf("  ⚠  project 字段更新失败 [%s]: %v\n", s.ID, err)
			}
			fmt.Printf("  ✓ [%s] %s → project=%s\n", s.ID, s.Title, defaultProject)
			migrated++
		}

		fmt.Printf("\n✅ 迁移完成：%d/%d 个会期已更新。\n", migrated, len(needsMigration))
		return nil
	},
}
