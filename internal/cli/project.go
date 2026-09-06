package cli

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
)

const defaultProjectGetFields = "name,archived,permalink_url"

var (
	projectListPaginate bool
	projectListFields   string
	projectGetFields    string
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Project commands",
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List projects in a workspace",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx := context.Background()
		ws, err := requireWorkspace(ctx, c)
		if err != nil {
			return err
		}
		q := url.Values{"workspace": []string{ws}}
		setOptFields(q, projectListFields)
		return runList(ctx, c, "/projects", q, projectListPaginate)
	},
}

var projectGetCmd = &cobra.Command{
	Use:   "get <gid>",
	Short: "Fetch a project",
	Long: `Fetch a project directly by GID without resolving a workspace.

The default fields are a compact projection of the project's identity, archive
state, and navigation link. Pass --fields to replace that projection, or pass
--fields "" to request Asana's raw default representation.`,
	Example: `  dharma project get 1234567890
  dharma project get 1234567890 --fields 'name,notes'
  dharma project get 1234567890 --fields 'name,owner.name,team.name'
  dharma project get 1234567890 --fields 'name,current_status_update.title,current_status_update.resource_subtype'
  dharma project get 1234567890 --fields ""`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		q := url.Values{}
		setOptFields(q, projectGetFields)
		return runGet(context.Background(), c, "/projects/"+args[0], q)
	},
}

func init() {
	projectListCmd.Flags().BoolVar(&projectListPaginate, "paginate", false, "fetch all pages")
	addFieldsFlag(projectListCmd, &projectListFields, "name,archived")
	addFieldsFlag(projectGetCmd, &projectGetFields, defaultProjectGetFields)
	projectCmd.AddCommand(projectListCmd, projectGetCmd)
}
