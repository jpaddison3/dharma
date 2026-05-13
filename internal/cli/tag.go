package cli

import (
	"context"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Tag commands",
}

var (
	tagListName     string
	tagListPaginate bool
)

var tagListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tags in a workspace; --name uses Asana's typeahead for fuzzy matching",
	RunE: func(cmd *cobra.Command, args []string) error {
		ws := resolveWorkspace()
		if ws == "" {
			return fmt.Errorf("workspace required (--workspace, ASANA_WORKSPACE, or config default)")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		if tagListName != "" {
			q := url.Values{"resource_type": []string{"tag"}, "query": []string{tagListName}}
			return runList(context.Background(), c, "/workspaces/"+ws+"/typeahead", q, false)
		}
		q := url.Values{"workspace": []string{ws}}
		return runList(context.Background(), c, "/tags", q, tagListPaginate)
	},
}

func init() {
	tagListCmd.Flags().StringVar(&tagListName, "name", "", "fuzzy match against tag names (uses typeahead; max ~20 results)")
	tagListCmd.Flags().BoolVar(&tagListPaginate, "paginate", false, "fetch all pages (ignored when --name is set)")
	tagCmd.AddCommand(tagListCmd)
}
