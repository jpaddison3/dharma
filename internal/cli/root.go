package cli

import (
	"fmt"
	"os"

	"github.com/jpaddison3/dharma/internal/client"
	"github.com/jpaddison3/dharma/internal/config"
	"github.com/spf13/cobra"
)

var (
	flagToken     string
	flagWorkspace string
	flagVerbose   bool
)

var rootCmd = &cobra.Command{
	Use:          "dharma",
	Short:        "Agent-friendly CLI for the Asana API",
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "Asana PAT (env: ASANA_TOKEN)")
	rootCmd.PersistentFlags().StringVar(&flagWorkspace, "workspace", "", "workspace gid (env: ASANA_WORKSPACE)")
	rootCmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "log HTTP requests to stderr")

	rootCmd.AddCommand(authCmd, apiCmd, userCmd, taskCmd, myTasksCmd, projectCmd, sectionCmd, tagCmd, workspaceCmd, attachmentCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func resolveToken() (string, error) {
	if flagToken != "" {
		return flagToken, nil
	}
	if t := os.Getenv("ASANA_TOKEN"); t != "" {
		return t, nil
	}
	cfg, err := config.Load()
	if err == nil && cfg.Token != "" {
		return cfg.Token, nil
	}
	return "", fmt.Errorf("no token found: run `dharma auth login`, set ASANA_TOKEN, or pass --token")
}

func resolveWorkspace() string {
	if flagWorkspace != "" {
		return flagWorkspace
	}
	if w := os.Getenv("ASANA_WORKSPACE"); w != "" {
		return w
	}
	if cfg, err := config.Load(); err == nil {
		return cfg.DefaultWorkspace
	}
	return ""
}

func newClient() (*client.Client, error) {
	token, err := resolveToken()
	if err != nil {
		return nil, err
	}
	c := client.New(token)
	c.Verbose = flagVerbose
	return c, nil
}
