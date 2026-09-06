package cli

import (
	"context"
	"net/url"
	"strconv"

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
			setOptFields(q, tagListFields)
			return runList(ctx, c, "/workspaces/"+ws+"/typeahead", q, false)
		}
		q := url.Values{"workspace": []string{ws}}
		setOptFields(q, tagListFields)
		return runList(ctx, c, "/tags", q, tagListPaginate)
	},
}

var (
	tagCreateName  string
	tagCreateColor string
)

var tagCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a tag in a workspace",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		if tagCreateName == "" {
			return usageErrorf("--name is required")
		}
		ctx := context.Background()
		ws, err := requireWorkspace(ctx, c)
		if err != nil {
			return err
		}
		body := map[string]interface{}{
			"name":      tagCreateName,
			"workspace": ws,
		}
		if tagCreateColor != "" {
			body["color"] = tagCreateColor
		}
		return runPost(ctx, c, "/tags", body)
	},
}

var tagGetFields string

var tagGetCmd = &cobra.Command{
	Use:   "get <gid>",
	Short: "Fetch a tag",
	Long:  `Fetch a tag by gid. Use --fields to choose the returned fields.`,
	Example: `  dharma tag get 1234567890
  dharma tag get 1234567890 --fields name,color,followers.name`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		q := url.Values{}
		setOptFields(q, tagGetFields)
		return runGet(context.Background(), c, "/tags/"+args[0], q)
	},
}

var (
	tagTasksFields   string
	tagTasksPaginate bool
	tagTasksLimit    int
)

var tagTasksCmd = &cobra.Command{
	Use:   "tasks <gid>",
	Short: "List every task associated with a tag",
	Long: `List every task associated with a tag.

Asana's tag-task endpoint does not support completion, modification, assignee,
project, or section filters, so this command always returns all tasks bearing
the tag. Use --fields to choose the returned fields and --paginate to follow
every result page.`,
	Example: `  dharma tag tasks 1234567890
  dharma tag tasks 1234567890 --fields name,tags.gid --paginate`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if tagTasksLimit < 0 || tagTasksLimit > 100 {
			return usageErrorf("--limit must be between 1 and 100, or 0 for the default")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		q := url.Values{}
		if tagTasksLimit > 0 {
			q.Set("limit", strconv.Itoa(tagTasksLimit))
		}
		setOptFields(q, tagTasksFields)
		return runList(context.Background(), c, "/tags/"+args[0]+"/tasks", q, tagTasksPaginate)
	},
}

func init() {
	tagListCmd.Flags().StringVar(&tagListName, "name", "", "fuzzy match against tag names (uses typeahead; max ~20 results)")
	tagListCmd.Flags().BoolVar(&tagListPaginate, "paginate", false, "fetch all pages (ignored when --name is set)")
	addFieldsFlag(tagListCmd, &tagListFields, "name,color")

	tagCreateCmd.Flags().StringVar(&tagCreateName, "name", "", "tag name (required)")
	tagCreateCmd.Flags().StringVar(&tagCreateColor, "color", "", "tag color")

	addFieldsFlag(tagGetCmd, &tagGetFields, "name,color")

	addFieldsFlag(tagTasksCmd, &tagTasksFields, defaultTaskListFields)
	tagTasksCmd.Flags().BoolVar(&tagTasksPaginate, "paginate", false, "fetch all pages")
	tagTasksCmd.Flags().IntVar(&tagTasksLimit, "limit", 0, "max items per page (1-100, default 100)")

	tagCmd.AddCommand(tagListCmd, tagCreateCmd, tagGetCmd, tagTasksCmd)
}
