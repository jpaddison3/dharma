package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

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

// requireWorkspace resolves the workspace gid, falling back to the API when
// nothing is configured: a token that sees exactly one workspace uses it; a
// token that sees several gets an error naming them rather than a silent pick.
func requireWorkspace(ctx context.Context, c *client.Client) (string, error) {
	if ws := resolveWorkspace(); ws != "" {
		return ws, nil
	}
	var workspaces []struct {
		GID  string `json:"gid"`
		Name string `json:"name"`
	}
	if err := c.Get(ctx, "/workspaces", nil, &workspaces); err != nil {
		return "", fmt.Errorf("resolving workspace: %w", err)
	}
	switch len(workspaces) {
	case 0:
		return "", fmt.Errorf("no workspaces visible to this token")
	case 1:
		return workspaces[0].GID, nil
	}
	names := make([]string, len(workspaces))
	for i, w := range workspaces {
		names[i] = fmt.Sprintf("%s (%s)", w.Name, w.GID)
	}
	return "", fmt.Errorf("multiple workspaces visible: %s — pass --workspace or set ASANA_WORKSPACE / default_workspace", strings.Join(names, ", "))
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
