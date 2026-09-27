package cmd

import (
	"fmt"
	"os"

	"github.com/hazim-j/agent-wow/internal/config"
	"github.com/spf13/cobra"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "agent-wow",
	Short: "AzerothCore WoW client designed for AI agent players.",
	Long: `
An opensource headless WoW client for AzerothCore.
Built for AI agents.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Init(cfgFile); err != nil {
			return fmt.Errorf("initialize config: %w", err)
		}
		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: config.* in the working directory)")
}
