package cmd

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/house-of-cards/hoc/internal/store"
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
}

type billSpec struct {
	ID        string   `toml:"id"`
	Title     string   `toml:"title"`
	Motion    string   `toml:"motion"`
	Portfolio string   `toml:"portfolio"`
	DependsOn []string `toml:"depends_on"`
}

// sessionCmd represents the session command
var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "管理 Session（会期）",
	Long:  "会期管理命令：开启、状态、解散",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	sessionCmd.AddCommand(sessionOpenCmd)
	sessionCmd.AddCommand(sessionStatusCmd)
	sessionCmd.AddCommand(sessionDissolveCmd)
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

		// Generate session ID.
		sid := shortID("session")

		// Create session in DB.
		sess := &store.Session{
			ID:       sid,
			Title:    spec.Session.Title,
			Topology: spec.Session.Topology,
			Status:   "active",
		}
		if err := db.CreateSession(sess); err != nil {
			return fmt.Errorf("create session: %w", err)
		}

		fmt.Printf("✅ 会期已开启\n")
		fmt.Printf("   ID:       %s\n", sid)
		fmt.Printf("   标题:     %s\n", spec.Session.Title)
		fmt.Printf("   拓扑:     %s\n", spec.Session.Topology)
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
			}
			if err := db.CreateBill(bill); err != nil {
				return fmt.Errorf("create bill %s: %w", bs.ID, err)
			}
			portfolioNote := ""
			if bs.Portfolio != "" {
				portfolioNote = fmt.Sprintf(" [portfolio: %s]", bs.Portfolio)
			}
			fmt.Printf("   📄 [%s] %s%s\n", billID, bs.Title, portfolioNote)
		}

		fmt.Printf("\n使用 `hoc bill list` 查看议案，`hoc bill assign <id> <minister>` 分配议案。\n")
		return nil
	},
}

var sessionStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "查看所有会期状态",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		sessions, err := db.ListSessions()
		if err != nil {
			return fmt.Errorf("list sessions: %w", err)
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
			}
			fmt.Printf("%s [%s] %s  (拓扑: %s)\n", statusIcon, s.Status, s.Title, s.Topology)
			fmt.Printf("   ID: %s  |  开启: %s\n", s.ID, s.CreatedAt.Format(time.RFC3339))

			// List bills for this session.
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
