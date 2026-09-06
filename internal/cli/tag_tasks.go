package cli

import (
	"context"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

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
	addFieldsFlag(tagTasksCmd, &tagTasksFields, defaultTaskListFields)
	tagTasksCmd.Flags().BoolVar(&tagTasksPaginate, "paginate", false, "fetch all pages")
	tagTasksCmd.Flags().IntVar(&tagTasksLimit, "limit", 0, "max items per page (1-100, default 100)")
	tagCmd.AddCommand(tagTasksCmd)
}
