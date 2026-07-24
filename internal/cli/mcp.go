package cli

import (
	"context"

	"github.com/jpaddison3/dharma/internal/mcpserver"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serve MCP over stdio (for ChatGPT desktop / Codex)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return mcpserver.Serve(context.Background(), version)
	},
}
