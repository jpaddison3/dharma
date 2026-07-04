package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jpaddison3/dharma/internal/client"
	"github.com/jpaddison3/dharma/internal/config"
	"github.com/jpaddison3/dharma/internal/output"
	"github.com/spf13/cobra"
)

var (
	flagToken     string
	flagWorkspace string
	flagVerbose   bool
)

// commandRan is set once a command body is reached (after flag/arg parsing
// succeeds), letting Execute tell a usage/parse error apart from an
// operational one — see classifyError.
var commandRan bool

var rootCmd = &cobra.Command{
	Use:           "dharma",
	Short:         "Agent-friendly CLI for the Asana API",
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		commandRan = true
	},
}

// UsageError marks an input problem (missing or conflicting flags, bad
// arguments) that a command detects in its own body, so Execute exits 3 —
// matching how cobra classifies the parse errors it catches itself.
type UsageError struct{ msg string }

func (e *UsageError) Error() string { return e.msg }

func usageErrorf(format string, a ...interface{}) error {
	return &UsageError{msg: fmt.Sprintf(format, a...)}
}

// AuthError marks a missing credential so Execute exits 2, the same code a
// rejected token (HTTP 401) produces.
type AuthError struct{ msg string }

func (e *AuthError) Error() string { return e.msg }

func init() {
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "Asana PAT (env: ASANA_TOKEN)")
	rootCmd.PersistentFlags().StringVar(&flagWorkspace, "workspace", "", "workspace gid (env: ASANA_WORKSPACE)")
	rootCmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "log HTTP requests to stderr")

	rootCmd.AddCommand(authCmd, apiCmd, userCmd, taskCmd, myTasksCmd, projectCmd, sectionCmd, tagCmd, workspaceCmd, attachmentCmd)
}

// errorEnvelope is the failure shape printed to stdout: an `ok:false`
// discriminator plus a structured error, so a consumer can branch on `.ok`
// and read `.error.http_status` / `.error.help` without scraping stderr.
type errorEnvelope struct {
	OK    bool         `json:"ok"`
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Help       string `json:"help,omitempty"`
}

func Execute() {
	err := rootCmd.Execute()
	if err == nil {
		return
	}
	code, payload := classifyError(err)
	// Machine-readable envelope on stdout; one human line on stderr.
	_ = output.Print(os.Stdout, errorEnvelope{OK: false, Error: payload})
	fmt.Fprintln(os.Stderr, "Error: "+payload.Message)
	os.Exit(code)
}

// classifyError maps an error to (exit code, payload). Codes match the sibling
// gdoc CLI: 1 = API/operational, 2 = auth, 3 = usage.
func classifyError(err error) (int, errorPayload) {
	var usageErr *UsageError
	if errors.As(err, &usageErr) {
		return 3, errorPayload{Message: usageErr.Error()}
	}
	if !commandRan {
		// Cobra rejected the invocation before any command body ran: unknown
		// command/flag or wrong argument count.
		return 3, errorPayload{Message: err.Error()}
	}
	var authErr *AuthError
	if errors.As(err, &authErr) {
		return 2, errorPayload{Message: authErr.Error()}
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		code := 1
		if apiErr.IsAuth() {
			code = 2
		}
		msg := apiErr.Message()
		if msg == "" {
			// Asana returned neither structured error nor body — keep the
			// status so the message is never empty.
			msg = apiErr.Error()
		}
		return code, errorPayload{
			Message:    msg,
			HTTPStatus: apiErr.StatusCode,
			Help:       apiErr.HelpText(),
		}
	}
	return 1, errorPayload{Message: err.Error()}
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
	return "", &AuthError{msg: "no token found: run `dharma auth login`, set ASANA_TOKEN, or pass --token"}
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
