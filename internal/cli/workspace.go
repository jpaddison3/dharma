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
		setOptFields(q, workspaceListFields)
		return runList(context.Background(), c, "/workspaces", q, workspaceListPaginate)
	},
}

func init() {
	workspaceListCmd.Flags().BoolVar(&workspaceListPaginate, "paginate", false, "fetch all pages")
	addFieldsFlag(workspaceListCmd, &workspaceListFields, "name")
	workspaceCmd.AddCommand(workspaceListCmd)
}
