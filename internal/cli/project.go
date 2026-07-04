package cli

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
)

var (
	projectListPaginate bool
	projectListFields   string
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
		if projectListFields != "" {
			q.Set("opt_fields", projectListFields)
		}
		return runList(ctx, c, "/projects", q, projectListPaginate)
	},
}

func init() {
	projectListCmd.Flags().BoolVar(&projectListPaginate, "paginate", false, "fetch all pages")
	projectListCmd.Flags().StringVar(&projectListFields, "fields", "name,archived", "opt_fields (curated default; pass --fields \"\" for Asana's raw fields)")
	projectCmd.AddCommand(projectListCmd)
}
