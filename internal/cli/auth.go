package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jpaddison3/dharma/internal/client"
	"github.com/jpaddison3/dharma/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Store a personal access token",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprint(os.Stderr, "Paste your Asana PAT (https://app.asana.com/0/my-apps): ")
		token, err := readSecret(os.Stdin)
		if err != nil {
			return err
		}
		if token == "" {
			return fmt.Errorf("token is empty")
		}
		c := client.New(token)
		var me struct {
			GID  string `json:"gid"`
			Name string `json:"name"`
		}
		if err := c.Get(context.Background(), "/users/me", nil, &me); err != nil {
			return fmt.Errorf("token check failed: %w", err)
		}
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		cfg.Token = token
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Authenticated as %s (%s)\n", me.Name, me.GID)
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current auth status",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runGet(context.Background(), c, "/users/me", nil)
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear the stored token",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		cfg.Token = ""
		return config.Save(cfg)
	},
}

// readSecret reads a single line from f. When f is a TTY it uses term.ReadPassword
// so the input isn't echoed; otherwise it falls back to a buffered read so piped
// input (e.g. `echo $PAT | dharma auth login`) still works.
func readSecret(f *os.File) (string, error) {
	fd := int(f.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := bufio.NewReader(f).ReadString('\n')
	trimmed := strings.TrimSpace(line)
	if err != nil && trimmed == "" {
		return "", err
	}
	return trimmed, nil
}

func init() {
	authCmd.AddCommand(authLoginCmd, authStatusCmd, authLogoutCmd)
}
