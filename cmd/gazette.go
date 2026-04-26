package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// gazetteCmd represents the gazette command.
var gazetteCmd = &cobra.Command{
	Use:   "gazette",
	Short: "Gazette（公报）管理",
	Long:  "公报管理命令：查看公报流转",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

//nolint:gochecknoinits // Cobra convention: register subcommands in init().
func init() {
	gazetteCmd.AddCommand(gazetteListCmd)
	gazetteCmd.AddCommand(gazetteShowCmd)
	gazetteCmd.AddCommand(gazetteTemplateCmd)

	gazetteListCmd.Flags().String("minister", "", "按部长 ID 过滤")
	gazetteListCmd.Flags().String("bill", "", "按议案 ID 过滤")

	gazetteTemplateCmd.Flags().String("type", "completion", "模板类型: completion, handoff, help, review, conflict")
}

var gazetteListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有公报",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer func() { _ = db.Close() }()

		ministerFilter, _ := cmd.Flags().GetString("minister")
		billFilter, _ := cmd.Flags().GetString("bill")

		if billFilter != "" {
			gs, err := db.ListGazettesForBill(billFilter)
			if err != nil {
				return fmt.Errorf("list gazettes: %w", err)
			}
			if len(gs) == 0 {
				fmt.Printf("暂无议案 [%s] 的公报\n", billFilter)
				return nil
			}
			fmt.Printf("📰 议案 [%s] 公报 (%d 份):\n", billFilter, len(gs))
			fmt.Println("─────────────────────────────────────────")
			for _, g := range gs {
				printGazette(g.ID, g.Type.String, g.FromMinister.String, g.ToMinister.String, g.BillID.String, g.Summary, g.CreatedAt)
			}
			return nil
		}

		if ministerFilter != "" {
			gs, err := db.ListGazettesForMinister(ministerFilter)
			if err != nil {
				return fmt.Errorf("list gazettes: %w", err)
			}
			if len(gs) == 0 {
				fmt.Printf("暂无部长 [%s] 的公报\n", ministerFilter)
				return nil
			}
			fmt.Printf("📰 部长 [%s] 公报 (%d 份):\n", ministerFilter, len(gs))
			fmt.Println("─────────────────────────────────────────")
			for _, g := range gs {
				printGazette(g.ID, g.Type.String, g.FromMinister.String, g.ToMinister.String, g.BillID.String, g.Summary, g.CreatedAt)
			}
			return nil
		}

		// All gazettes.
		gs, err := db.ListGazettes()
		if err != nil {
			return fmt.Errorf("list gazettes: %w", err)
		}
		if len(gs) == 0 {
			fmt.Println("暂无公报。议案完成后公报会自动记录。")
			return nil
		}
		fmt.Printf("📰 公报列表 (%d 份):\n", len(gs))
		fmt.Println("─────────────────────────────────────────")
		for _, g := range gs {
			printGazette(g.ID, g.Type.String, g.FromMinister.String, g.ToMinister.String, g.BillID.String, g.Summary, g.CreatedAt)
		}
		return nil
	},
}

var gazetteShowCmd = &cobra.Command{
	Use:   "show [gazette-id]",
	Short: "查看公报详情",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer func() { _ = db.Close() }()

		// Fetch all and find by ID prefix.
		gs, err := db.ListGazettes()
		if err != nil {
			return fmt.Errorf("list gazettes: %w", err)
		}

		target := args[0]
		for _, g := range gs {
			if g.ID == target || (len(g.ID) > len(target) && g.ID[:len(target)] == target) {
				fmt.Println("📰 公报详情")
				fmt.Println("────────────────────────────────")
				fmt.Printf("ID:      %s\n", g.ID)
				fmt.Printf("类型:    %s\n", orDash(g.Type.String))
				fmt.Printf("发自:    %s\n", orDash(g.FromMinister.String))
				fmt.Printf("致:      %s\n", orDash(g.ToMinister.String))
				fmt.Printf("议案:    %s\n", orDash(g.BillID.String))
				fmt.Printf("时间:    %s\n", g.CreatedAt.Format(time.RFC3339))
				if g.ReadAt.Valid {
					fmt.Printf("已读:    %s\n", g.ReadAt.Time.Format(time.RFC3339))
				}
				fmt.Printf("\n内容:\n%s\n", g.Summary)
				if g.Artifacts.String != "" && g.Artifacts.String != "null" {
					fmt.Printf("\n产物:\n%s\n", g.Artifacts.String)
				}
				return nil
			}
		}
		return fmt.Errorf("gazette not found: %s", target)
	},
}

// ─── Phase 3: B-2 Gazette Template ──────────────────────────────────────────

var gazetteTemplates = map[string]string{
	"completion": `summary = "简要描述完成的工作（1-2句话）"

[contracts]
# 下游部长需要知道的接口/契约
# 例如: "api.go" = "新增 UserService 接口"
"example.go" = "描述"

[artifacts]
# 新增或修改的文件
# 例如: "internal/handler.go" = "新增"
"file.go" = "新增/修改"

[assumptions]
# 关键假设或风险
# 例如: "api-version" = "假设下游使用 v2 API"
key = "assumption"
`,
	"handoff": `# Gazette: handoff
> From: <minister-id> | Bill: <bill-id> | Date: <date>

## 交接说明
[描述当前进度和接手注意事项]

## 已完成工作
- [ ] 列出已完成的部分

## 待完成工作
- [ ] 列出需要接手完成的部分

## 关键文件
- ` + "`file/path`" + ` — 说明

## 注意事项
[接手人需要知道的关键信息]
`,
	"help": `# Gazette: help
> From: <minister-id> | Bill: <bill-id> | Date: <date>

## 问题描述
[清晰描述遇到的问题]

## 已尝试的方案
1. [方案1] — 结果
2. [方案2] — 结果

## 期望的帮助
[需要什么类型的帮助]

## 相关上下文
- 文件: ` + "`file/path`" + `
- 错误信息: ` + "`...`" + `
`,
	"review": `# Gazette: review
> From: committee | Bill: <bill-id> | Date: <date>

## 审查结论
- [ ] 通过 (PASS)
- [ ] 退回 (FAIL)

## 质量评分
评分: X.XX / 1.00

## 审查意见
[详细的审查意见]

## 建议修改
- [ ] 修改项 1
- [ ] 修改项 2
`,
	"conflict": `# Gazette: conflict
> From: privy-council | Bill: <bill-id> | Date: <date>

## 冲突描述
[描述合并冲突的具体内容]

## 冲突文件
- ` + "`file/path`" + ` — 冲突描述

## 建议解决方案
[如何解决冲突]

## 涉及分支
- 分支 A: ` + "`branch-name`" + `
- 分支 B: ` + "`branch-name`" + `
`,
}

var gazetteTemplateCmd = &cobra.Command{
	Use:   "template",
	Short: "输出 Gazette 模板",
	Long: `输出指定类型的 Gazette 模板。

类型：
  completion  完成公报（TOML 格式，匹配 .done 文件结构）
  handoff     交接公报（Markdown）
  help        求助公报（Markdown）
  review      审查公报（Markdown）
  conflict    冲突公报（Markdown）

示例：
  hoc gazette template                        # 默认输出 completion 模板
  hoc gazette template --type handoff         # 输出 handoff 模板`,
	RunE: func(cmd *cobra.Command, args []string) error {
		typ, _ := cmd.Flags().GetString("type")
		if typ == "" {
			typ = "completion"
		}

		tmpl, ok := gazetteTemplates[typ]
		if !ok {
			valid := make([]string, 0, len(gazetteTemplates))
			for k := range gazetteTemplates {
				valid = append(valid, k)
			}
			return fmt.Errorf("未知模板类型 %q，可用类型: %v", typ, valid)
		}

		fmt.Print(tmpl)
		return nil
	},
}

func printGazette(id, typ, from, to, billID, summary string, createdAt time.Time) {
	typeIcon := "📄"
	switch typ {
	case "completion":
		typeIcon = "✅"
	case "handoff":
		typeIcon = "🔄"
	case "help":
		typeIcon = "🆘"
	case "review":
		typeIcon = "🔍"
	case "conflict":
		typeIcon = "⚠️"
	}
	fromStr := orDash(from)
	toStr := orDash(to)
	billStr := orDash(billID)
	fmt.Printf("%s [%s] %s → %s  (议案: %s)\n", typeIcon, typ, fromStr, toStr, billStr)
	fmt.Printf("   %s\n", truncate(summary, 80))
	fmt.Printf("   ID: %s  |  %s\n", id, createdAt.Format("2006-01-02 15:04"))
	fmt.Println()
}
