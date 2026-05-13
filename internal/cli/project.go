package cli

import (
	"context"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

var projectListPaginate bool

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
		ws := resolveWorkspace()
		if ws == "" {
			return fmt.Errorf("workspace required (--workspace, ASANA_WORKSPACE, or config default)")
		}
		q := url.Values{"workspace": []string{ws}}
		return runList(context.Background(), c, "/projects", q, projectListPaginate)
	},
}

func init() {
	projectListCmd.Flags().BoolVar(&projectListPaginate, "paginate", false, "fetch all pages")
	projectCmd.AddCommand(projectListCmd)
}
