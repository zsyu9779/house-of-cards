package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示详细版本信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("House of Cards (hoc) %s\n", Version)
		fmt.Printf("  Git Commit:  %s\n", GitCommit)
		fmt.Printf("  Build Time:  %s\n", BuildTime)
		fmt.Printf("  Go Version:  %s\n", runtime.Version())
		fmt.Printf("  OS/Arch:     %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}
