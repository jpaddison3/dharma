package cli

import (
	"context"
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
	tagListFields   string
)

var tagListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tags in a workspace; --name uses Asana's typeahead for fuzzy matching",
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
		if tagListName != "" {
			q := url.Values{"resource_type": []string{"tag"}, "query": []string{tagListName}}
			if tagListFields != "" {
				q.Set("opt_fields", tagListFields)
			}
			return runList(ctx, c, "/workspaces/"+ws+"/typeahead", q, false)
		}
		q := url.Values{"workspace": []string{ws}}
		if tagListFields != "" {
			q.Set("opt_fields", tagListFields)
		}
		return runList(ctx, c, "/tags", q, tagListPaginate)
	},
}

func init() {
	tagListCmd.Flags().StringVar(&tagListName, "name", "", "fuzzy match against tag names (uses typeahead; max ~20 results)")
	tagListCmd.Flags().BoolVar(&tagListPaginate, "paginate", false, "fetch all pages (ignored when --name is set)")
	tagListCmd.Flags().StringVar(&tagListFields, "fields", "name,color", "opt_fields (curated default; pass --fields \"\" for Asana's raw fields)")
	tagCmd.AddCommand(tagListCmd)
}
