package cli

import (
	"context"

	"github.com/spf13/cobra"
)

var workspaceListPaginate bool

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Workspace commands",
}

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workspaces you can access",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runList(context.Background(), c, "/workspaces", nil, workspaceListPaginate)
	},
}

func init() {
	workspaceListCmd.Flags().BoolVar(&workspaceListPaginate, "paginate", false, "fetch all pages")
	workspaceCmd.AddCommand(workspaceListCmd)
}
