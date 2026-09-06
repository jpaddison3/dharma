package cli

import (
	"context"

	"github.com/spf13/cobra"
)

var taskRemoveTagTag string

var taskRemoveTagCmd = &cobra.Command{
	Use:     "remove-tag <gid>",
	Short:   "Remove a tag association from a task",
	Long:    `Remove a tag association from a task. The tag itself is not deleted.`,
	Example: `  dharma task remove-tag 1234567890 --tag 9876543210`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if taskRemoveTagTag == "" {
			return usageErrorf("--tag is required (a tag gid)")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPost(context.Background(), c, "/tasks/"+args[0]+"/removeTag", map[string]interface{}{"tag": taskRemoveTagTag})
	},
}

func init() {
	taskRemoveTagCmd.Flags().StringVar(&taskRemoveTagTag, "tag", "", "tag gid (required)")
	taskCmd.AddCommand(taskRemoveTagCmd)
}
