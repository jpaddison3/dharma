package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/jpaddison3/dharma/internal/mcpserver"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serve MCP over stdio (for ChatGPT desktop / Codex)",
	RunE: func(cmd *cobra.Command, args []string) error {
		// The server re-execs this binary per tool call, and the subprocess
		// sees only the environment and the config file — never this
		// process's parsed flags. Promote the two globals that decide who the
		// calls are made as, so `dharma --token X mcp` isn't silently
		// unauthenticated. --output and --verbose are deliberately not
		// promoted: tool results are contractually JSON, and verbose HTTP
		// logging on the subprocess's stderr would reach the model as a
		// per-call caveat.
		if flagToken != "" {
			os.Setenv("ASANA_TOKEN", flagToken)
		}
		if flagWorkspace != "" {
			os.Setenv("ASANA_WORKSPACE", flagWorkspace)
		}
		// stdout is the JSON-RPC stream in this mode, so this command reports
		// its own failure instead of returning it to Execute, whose error
		// envelope would be written onto that stream.
		if err := mcpserver.Serve(context.Background(), version); err != nil {
			fmt.Fprintln(os.Stderr, "Error: "+err.Error())
			os.Exit(1)
		}
		return nil
	},
}
