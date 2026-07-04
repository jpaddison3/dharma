package cli

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
)

var (
	workspaceListPaginate bool
	workspaceListFields   string
)

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
		q := url.Values{}
		if workspaceListFields != "" {
			q.Set("opt_fields", workspaceListFields)
		}
		return runList(context.Background(), c, "/workspaces", q, workspaceListPaginate)
	},
}

func init() {
	workspaceListCmd.Flags().BoolVar(&workspaceListPaginate, "paginate", false, "fetch all pages")
	workspaceListCmd.Flags().StringVar(&workspaceListFields, "fields", "name", "opt_fields (curated default; pass --fields \"\" for Asana's raw fields)")
	workspaceCmd.AddCommand(workspaceListCmd)
}
