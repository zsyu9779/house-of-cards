// Package cmd CLI commands
package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/house-of-cards/hoc/internal/config"
	"github.com/house-of-cards/hoc/internal/otel"
	"github.com/spf13/cobra"
)

var (
	Version   = "0.1.0"
	GitCommit = "dev"
	verbose   bool
	quiet     bool
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "hoc",
	Short: "House of Cards - AI Agent 协作框架",
	Long: `House of Cards 是一个 AI Agent 协作框架，使用政府隐喻构建多 Agent 编排系统。

核心概念：
  Speaker（议长）- 编排决策者
  Minister（部长）- 执行 Agent
  Whip（党鞭）- 系统推进力
  Gazette（公报）- 信息凝练层
  Hansard（议事录）- 审计记录`,
	Version:           fmt.Sprintf("%s (%s)", Version, GitCommit),
	PersistentPreRunE: initLogging,
}

// initLogging sets up the global slog handler and observability provider.
// Default level: INFO (show operational logs without --verbose).
// --verbose: DEBUG (detailed internal tracing).
// --quiet:   ERROR (suppress INFO/WARN, only show errors).
func initLogging(_ *cobra.Command, _ []string) error {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	} else if quiet {
		level = slog.LevelError
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))

	// Initialise the global observability provider from config.
	hocHome := config.GetHOCHome()
	if cfg, err := config.LoadConfig(hocHome); err == nil {
		obs := cfg.Observability
		otel.InitFromConfig(otel.ExporterConfig{
			Type:         obs.Exporter,
			OTLPEndpoint: obs.OTLPEndpoint,
			ServiceName:  obs.ServiceName,
		})
	} else {
		// Fallback to nop if config cannot be loaded.
		otel.InitFromConfig(otel.DefaultExporterConfig())
	}

	return nil
}

// Execute adds all child commands to the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "启用详细日志输出（DEBUG 级别）")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "静默模式，只输出错误（ERROR 级别）")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(ministersCmd)
	rootCmd.AddCommand(sessionCmd)
	rootCmd.AddCommand(billCmd)
	rootCmd.AddCommand(whipCmd)
	rootCmd.AddCommand(speakerCmd)
	rootCmd.AddCommand(cabinetCmd)
	rootCmd.AddCommand(floorCmd)
	rootCmd.AddCommand(gazetteCmd)
	rootCmd.AddCommand(hansardCmd)
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(privyCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(formulaCmd)
	rootCmd.AddCommand(eventsCmd)
}
