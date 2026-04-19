package cmd

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/house-of-cards/hoc/internal/store"
	"github.com/spf13/cobra"
)

// cabinetCmd represents the cabinet command.
var cabinetCmd = &cobra.Command{
	Use:   "cabinet",
	Short: "Cabinet（内阁）管理",
	Long:  "内阁管理命令：查看、改组",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

//nolint:gochecknoinits // Cobra convention: register subcommands in init().
func init() {
	cabinetCmd.AddCommand(cabinetListCmd)
	cabinetCmd.AddCommand(cabinetReshuffleCmd)

	cabinetReshuffleCmd.Flags().Bool("confirm", false, "实际执行改组（默认为预演模式）")
	cabinetReshuffleCmd.Flags().Int("max-per-minister", 1, "每位部长最多分配的议案数量（Phase 3E）")
}

var cabinetListCmd = &cobra.Command{
	Use:   "list",
	Short: "查看内阁花名册（含 Hansard 成功率）",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		ministers, err := db.ListMinisters()
		if err != nil {
			return fmt.Errorf("list ministers: %w", err)
		}

		fmt.Println("🏛  内阁花名册 (Cabinet)")
		fmt.Println("══════════════════════════════════════════════════════")

		if len(ministers) == 0 {
			fmt.Println("  (暂无部长。使用 `hoc minister appoint` 任命部长)")
			return nil
		}

		for _, m := range ministers {
			statusIcon := "⚪"
			switch m.Status {
			case "working":
				statusIcon = "🟢"
			case "idle":
				statusIcon = "🟡"
			case "stuck":
				statusIcon = "🔴"
			}

			portfolio := parsePortfolio(m.Skills)
			enacted, total, _ := db.HansardSuccessRate(m.ID)

			rateStr := "新任"
			if total > 0 {
				pct := int(float64(enacted) / float64(total) * 100)
				rateStr = fmt.Sprintf("%d/%d (%d%%)", enacted, total, pct)
			}

			fmt.Printf("\n%s  %s\n", statusIcon, m.Title)
			fmt.Printf("   ID:        %s\n", m.ID)
			fmt.Printf("   Runtime:   %s\n", m.Runtime)
			fmt.Printf("   Portfolio: %s\n", portfolioStr(portfolio))
			fmt.Printf("   议案通过:  %s\n", rateStr)
			fmt.Printf("   状态:      %s\n", m.Status)

			// Show current bills.
			bills, _ := db.GetBillsByAssignee(m.ID)
			activeBills := filterActiveBills(bills)
			if len(activeBills) > 0 {
				fmt.Printf("   当前议案:  ")
				for i, b := range activeBills {
					if i > 0 {
						fmt.Printf(", ")
					}
					fmt.Printf("[%s] %s", b.Status, b.Title)
				}
				fmt.Println()
			}
		}
		fmt.Println("\n══════════════════════════════════════════════════════")
		fmt.Printf("共 %d 位部长\n", len(ministers))
		return nil
	},
}

var cabinetReshuffleCmd = &cobra.Command{
	Use:   "reshuffle",
	Short: "内阁改组：将 draft 议案派发给最匹配的空闲部长",
	Long: `扫描所有 draft 状态（未分配）的议案，根据技能匹配找到最佳空闲部长并派发。

默认为预演模式（dry run），使用 --confirm 执行实际改组。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		confirm, _ := cmd.Flags().GetBool("confirm")
		maxPerMinister, _ := cmd.Flags().GetInt("max-per-minister")
		if maxPerMinister < 1 {
			maxPerMinister = 1
		}

		// Get all draft bills without assignee.
		bills, err := db.ListBills()
		if err != nil {
			return fmt.Errorf("list bills: %w", err)
		}

		var draftBills []*store.Bill
		for _, b := range bills {
			if b.Status == "draft" && b.Assignee.String == "" {
				draftBills = append(draftBills, b)
			}
		}

		if len(draftBills) == 0 {
			fmt.Println("✅ 无待派发议案，内阁运作正常。")
			return nil
		}

		// Get all idle ministers.
		idleMinisters, err := db.ListMinistersWithStatus("idle")
		if err != nil {
			return fmt.Errorf("list idle ministers: %w", err)
		}

		if len(idleMinisters) == 0 {
			fmt.Printf("⚠  共 %d 份待派发议案，但暂无空闲部长。\n", len(draftBills))
			fmt.Println("  使用 `hoc minister summon <id>` 传召部长。")
			return nil
		}

		fmt.Printf("🔄 内阁改组预案\n")
		fmt.Println("══════════════════════════════════════")
		fmt.Printf("  待派发议案:    %d 份\n", len(draftBills))
		fmt.Printf("  空闲部长:      %d 位\n", len(idleMinisters))
		fmt.Printf("  每部长上限:    %d 份议案\n", maxPerMinister)
		fmt.Println()

		type assignment struct {
			bill     *store.Bill
			minister *store.Minister
		}
		var assignments []assignment

		// Match bills to ministers by portfolio, respecting maxPerMinister.
		// ministerCount tracks how many bills each minister has been assigned in this reshuffle.
		ministerCount := map[string]int{}
		for _, bill := range draftBills {
			best := findBestMinisterWithLimit(bill, idleMinisters, ministerCount, maxPerMinister, db)
			if best == nil {
				fmt.Printf("  ⚠  议案 [%s] \"%s\" — 无匹配部长\n", bill.ID, bill.Title)
				continue
			}
			ministerCount[best.ID]++
			assignments = append(assignments, assignment{bill: bill, minister: best})
		}

		fmt.Println("  派发方案:")
		for _, a := range assignments {
			portfolio := a.bill.Portfolio.String
			if portfolio == "" {
				portfolio = "通用"
			}
			enacted, total, _ := db.HansardSuccessRate(a.minister.ID)
			rate := "-"
			if total > 0 {
				rate = fmt.Sprintf("%d/%d", enacted, total)
			}
			fmt.Printf("  📄 [%s] \"%s\" → %s  (portfolio: %s, 成功率: %s)\n",
				a.bill.ID, a.bill.Title, a.minister.Title, portfolio, rate)
		}

		if !confirm {
			fmt.Println("\n  ℹ  预演模式。使用 --confirm 执行实际改组。")
			return nil
		}

		// Execute assignments.
		fmt.Println()
		for _, a := range assignments {
			if err := db.AssignBill(a.bill.ID, a.minister.ID); err != nil {
				slog.Warn("派发议案失败", "bill_id", a.bill.ID, "err", err)
				continue
			}
			if err := db.UpdateBillStatus(a.bill.ID, "reading"); err != nil {
				slog.Warn("更新议案状态失败", "err", err)
			}

			// Create handoff gazette.
			g := &store.Gazette{
				ID:           shortID("gazette"),
				ToMinister:   store.NullString(a.minister.ID),
				BillID:       store.NullString(a.bill.ID),
				FromMinister: store.NullString("cabinet"),
				Type:         store.NullString("handoff"),
				Summary: fmt.Sprintf("内阁改组令：议案 [%s] \"%s\" 派发给 %s。\n请运行: hoc minister summon %s --bill %s --project <project>",
					a.bill.ID, a.bill.Title, a.minister.Title, a.minister.ID, a.bill.ID),
			}
			warnIfErr("create gazette", db.CreateGazette(g), "gazette_id", g.ID,
				"minister_id", a.minister.ID, "bill_id", a.bill.ID)

			fmt.Printf("  ✓ 议案 [%s] → %s\n", a.bill.ID, a.minister.Title)
		}
		fmt.Printf("\n✅ 内阁改组完成，共派发 %d 份议案。\n", len(assignments))
		return nil
	},
}

// findBestMinisterWithLimit finds the best idle minister for a bill based on portfolio match
// and success rate, while respecting the per-minister assignment limit.
// Phase 3E — 负载均衡 + 批量分配.
func findBestMinisterWithLimit(bill *store.Bill, idle []*store.Minister, counts map[string]int, maxPerMinister int, db *store.DB) *store.Minister {
	portfolio := bill.Portfolio.String

	var best *store.Minister
	bestScore := -1

	for _, m := range idle {
		// Enforce per-minister limit.
		if counts[m.ID] >= maxPerMinister {
			continue
		}

		var score int
		switch {
		case portfolio == "":
			score = 1 // Any minister matches a bill with no portfolio requirement.
		case ministerHasPortfolio(m.Skills, portfolio):
			score = 10
		default:
			continue // Skip ministers without matching skill.
		}

		// Bonus for higher success rate.
		enacted, total, _ := db.HansardSuccessRate(m.ID)
		if total > 0 {
			score += int(float64(enacted) / float64(total) * 5)
		}

		// Prefer ministers with fewer current assignments (load balancing).
		score -= counts[m.ID] * 2

		if score > bestScore {
			bestScore = score
			best = m
		}
	}

	return best
}

// ministerHasPortfolio checks if minister's skills JSON contains the given portfolio.
func ministerHasPortfolio(skillsJSON, portfolio string) bool {
	if skillsJSON == "" {
		return false
	}
	var skills []string
	if err := json.Unmarshal([]byte(skillsJSON), &skills); err != nil {
		return strings.Contains(skillsJSON, portfolio)
	}
	for _, s := range skills {
		if strings.EqualFold(s, portfolio) {
			return true
		}
	}
	return false
}

// parsePortfolio parses the minister skills JSON into a slice.
func parsePortfolio(skillsJSON string) []string {
	if skillsJSON == "" {
		return nil
	}
	var skills []string
	if err := json.Unmarshal([]byte(skillsJSON), &skills); err != nil {
		return []string{skillsJSON}
	}
	return skills
}

// portfolioStr formats portfolio skills for display.
func portfolioStr(skills []string) string {
	if len(skills) == 0 {
		return "(通用)"
	}
	return strings.Join(skills, ", ")
}

// filterActiveBills returns bills that are not in terminal states.
func filterActiveBills(bills []*store.Bill) []*store.Bill {
	var active []*store.Bill
	for _, b := range bills {
		switch b.Status {
		case "enacted", "royal_assent", "failed":
			// Skip terminal states.
		default:
			active = append(active, b)
		}
	}
	return active
}
