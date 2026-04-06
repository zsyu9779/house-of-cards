package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/house-of-cards/hoc/internal/config"
	"github.com/house-of-cards/hoc/internal/formula"
	"github.com/spf13/cobra"
)

// ─── hoc formula ──────────────────────────────────────────────────────────

var formulaCmd = &cobra.Command{
	Use:   "formula",
	Short: "Formula 工作流管理（hoc formula list/apply/status）",
	Long: `Formula 是预定义的工作流模板，用于自动化常见操作。

内置 Formula（可用 TOML 覆盖）：
  cleanup-chambers  清理长期空闲的议事厅
  auto-merge        自动合并已通过审查的议案分支
  sync-main         同步所有议事厅到最新 main 分支
  health-check      全局健康检查
  archive-session   归档已完成会期

用户自定义 Formula 目录：
  ~/.hoc/formulas/   或   <project>/.hoc/formulas/`,
}

// ─── hoc formula list ────────────────────────────────────────────────────

var formulaListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有可用 Formula",
	RunE:  runFormulaList,
}

func runFormulaList(_ *cobra.Command, _ []string) error {
	reg, err := loadFormulaRegistry()
	if err != nil {
		return err
	}

	formulas := reg.List()
	if len(formulas) == 0 {
		fmt.Println("（无可用 Formula）")
		return nil
	}

	fmt.Printf("%-22s  %-8s  %s\n", "名称", "触发器", "说明")
	fmt.Println(strings.Repeat("─", 70))
	for _, f := range formulas {
		trigger := f.Trigger
		if trigger == "" {
			trigger = "manual"
		}
		kind := "用户"
		if f.IsBuiltin() {
			kind = "内置"
		}
		fmt.Printf("%-22s  %-8s  [%s] %s\n", f.Name, trigger, kind, f.Description)
	}
	return nil
}

// ─── hoc formula apply ───────────────────────────────────────────────────

var (
	formulaDryRun bool
	formulaVars   []string
)

var formulaApplyCmd = &cobra.Command{
	Use:   "apply <formula-name>",
	Short: "执行指定 Formula",
	Args:  cobra.ExactArgs(1),
	RunE:  runFormulaApply,
}

func runFormulaApply(_ *cobra.Command, args []string) error {
	name := args[0]

	reg, err := loadFormulaRegistry()
	if err != nil {
		return err
	}

	f := reg.Get(name)
	if f == nil {
		return fmt.Errorf("Formula 不存在: %s（使用 `hoc formula list` 查看可用列表）", name)
	}

	// Parse --var key=value flags.
	vars := parseVarFlags(formulaVars)

	hocHome := config.GetHOCHome()
	opts := formula.ExecuteOpts{
		HocDir: filepath.Join(hocHome, ".hoc"),
		DryRun: formulaDryRun,
		Vars:   vars,
	}

	if formulaDryRun {
		fmt.Printf("▶ [dry-run] 执行 Formula: %s\n\n", f.Name)
	} else {
		fmt.Printf("▶ 执行 Formula: %s\n\n", f.Name)
	}

	result := formula.Execute(context.Background(), f, opts)
	printFormulaResult(result)

	if !result.Success {
		os.Exit(1)
	}
	return nil
}

// ─── hoc formula status ──────────────────────────────────────────────────

var formulaStatusCmd = &cobra.Command{
	Use:   "status <formula-name>",
	Short: "查看 Formula 信息（步骤列表）",
	Args:  cobra.ExactArgs(1),
	RunE:  runFormulaStatus,
}

func runFormulaStatus(_ *cobra.Command, args []string) error {
	name := args[0]

	reg, err := loadFormulaRegistry()
	if err != nil {
		return err
	}

	f := reg.Get(name)
	if f == nil {
		return fmt.Errorf("Formula 不存在: %s", name)
	}

	kind := "用户自定义"
	if f.IsBuiltin() {
		kind = "内置"
	}

	fmt.Printf("Formula:  %s  [%s]\n", f.Name, kind)
	fmt.Printf("说明:     %s\n", f.Description)
	fmt.Printf("触发器:   %s\n", f.Trigger)
	fmt.Printf("步骤数:   %d\n\n", len(f.Steps))

	for i, step := range f.Steps {
		mode := "顺序"
		if step.Parallel {
			mode = "并行"
		}
		fmt.Printf("  Step %d: %s（%s，%d 个 Action）\n",
			i+1, step.Name, mode, len(step.Actions))
		for j, a := range step.Actions {
			fmt.Printf("    %d.%d  [%s] %s", i+1, j+1, a.Type, a.Command)
			if len(a.Targets) > 0 {
				fmt.Printf("  → %s", strings.Join(a.Targets, ", "))
			}
			fmt.Println()
		}
	}
	return nil
}

// ─── helpers ─────────────────────────────────────────────────────────────

// loadFormulaRegistry builds a Registry from built-ins + user formula dirs.
func loadFormulaRegistry() (*formula.Registry, error) {
	hocHome := config.GetHOCHome()
	userDir := filepath.Join(hocHome, ".hoc", "formulas")
	return formula.LoadRegistryFromDirs(userDir)
}

// parseVarFlags converts ["key=value", ...] to map[string]string.
func parseVarFlags(flags []string) map[string]string {
	m := make(map[string]string, len(flags))
	for _, f := range flags {
		parts := strings.SplitN(f, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

// printFormulaResult prints a human-readable execution report.
func printFormulaResult(result *formula.RunResult) {
	status := "✅ 成功"
	if !result.Success {
		status = "❌ 失败"
	}

	fmt.Printf("结果: %s  (耗时: %s)\n\n", status, result.Duration().Round(1e6))

	for _, sr := range result.Steps {
		stepStatus := "✓"
		if !sr.Success {
			stepStatus = "✗"
		}
		fmt.Printf("  [%s] 步骤: %s\n", stepStatus, sr.Name)
		for _, ar := range sr.Actions {
			arStatus := "  ✓"
			if ar.Err != nil {
				arStatus = "  ✗"
			}
			fmt.Printf("    %s [%s] %s", arStatus, ar.Type, ar.Command)
			if ar.Target != "" {
				fmt.Printf(" @ %s", ar.Target)
			}
			fmt.Println()
			if ar.Err != nil {
				fmt.Printf("      错误: %v\n", ar.Err)
			}
			if ar.Output != "" {
				// Indent the output.
				for _, line := range strings.Split(strings.TrimRight(ar.Output, "\n"), "\n") {
					fmt.Printf("      %s\n", line)
				}
			}
		}
	}
}

//nolint:gochecknoinits // Cobra convention: register subcommands in init().
func init() {
	formulaApplyCmd.Flags().BoolVar(&formulaDryRun, "dry-run", false, "预览执行步骤，不实际运行")
	formulaApplyCmd.Flags().StringArrayVar(&formulaVars, "var", nil, "模板变量 key=value（可多次指定）")

	formulaCmd.AddCommand(formulaListCmd)
	formulaCmd.AddCommand(formulaApplyCmd)
	formulaCmd.AddCommand(formulaStatusCmd)
}
