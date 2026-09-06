package cli

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
)

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

func init() {
	addFieldsFlag(tagGetCmd, &tagGetFields, "name,color")
	tagCmd.AddCommand(tagGetCmd)
}
