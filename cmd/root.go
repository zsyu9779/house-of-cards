// Package cmd CLI commands
package cmd

import (
	"fmt"
	"os"

	"github.com/house-of-cards/hoc/internal/config"
	"github.com/house-of-cards/hoc/internal/logger"
	"github.com/house-of-cards/hoc/internal/otel"
	"github.com/spf13/cobra"
)

var (
	Version   = "0.2.0"
	GitCommit = "dev"
	BuildTime = "unknown"
	verbose   bool
	quiet     bool
	logLevel  string
	logFormat string
)

// rootCmd represents the base command.
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
	Version:           fmt.Sprintf("%s (%s, %s)", Version, GitCommit, BuildTime),
	PersistentPreRunE: initLogging,
}

// initLogging sets up the global slog handler and observability provider.
//
// Priority for level:
//  1. --log-level (explicit)
//  2. --verbose (=> debug) / --quiet (=> error)
//  3. config.toml [log].level
//  4. "info"
//
// Priority for format: --log-format > config.toml [log].format > "text".
func initLogging(_ *cobra.Command, _ []string) error {
	hocHome := config.GetHOCHome()
	cfg, cfgErr := config.LoadConfig(hocHome)

	cliLevel := logLevel
	if cliLevel == "" {
		switch {
		case verbose:
			cliLevel = "debug"
		case quiet:
			cliLevel = "error"
		}
	}

	var cfgLevel, cfgFormat string
	if cfgErr == nil {
		cfgLevel = cfg.Log.Level
		cfgFormat = cfg.Log.Format
	}

	logger.Init(logger.Options{
		Level:  logger.Resolve(cliLevel, cfgLevel, "info"),
		Format: logger.Resolve(logFormat, cfgFormat, "text"),
	})

	// Initialise the global observability provider from config.
	if cfgErr == nil {
		obs := cfg.Observability
		otel.InitFromConfig(otel.ExporterConfig{
			Type:         obs.Exporter,
			OTLPEndpoint: obs.OTLPEndpoint,
			ServiceName:  obs.ServiceName,
		})
	} else {
		otel.InitFromConfig(otel.DefaultExporterConfig())
	}

	return nil
}

// Execute adds all child commands to the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

//nolint:gochecknoinits // Cobra convention: register flags in init().
func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "启用详细日志输出（DEBUG 级别）")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "静默模式，只输出错误（ERROR 级别）")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "日志级别 (debug/info/warn/error)，优先于 --verbose/--quiet 与 config.toml")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "", "日志格式 (text/json)，优先于 config.toml")

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
	rootCmd.AddCommand(versionCmd)
}
