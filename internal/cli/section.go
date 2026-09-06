package cli

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
)

var sectionCmd = &cobra.Command{
	Use:   "section",
	Short: "Section commands",
}

var (
	sectionListProject  string
	sectionListPaginate bool
	sectionListFields   string
)

var sectionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sections in a project",
	RunE: func(cmd *cobra.Command, args []string) error {
		if sectionListProject == "" {
			return usageErrorf("--project is required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		q := url.Values{}
		setOptFields(q, sectionListFields)
		return runList(context.Background(), c, "/projects/"+sectionListProject+"/sections", q, sectionListPaginate)
	},
}

var sectionGetFields string

var sectionGetCmd = &cobra.Command{
	Use:   "get <gid>",
	Short: "Fetch a section",
	Args:  cobra.ExactArgs(1),
	Example: `  dharma section get 123
  dharma section get 123 --fields name,project.name`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		q := url.Values{}
		setOptFields(q, sectionGetFields)
		return runGet(context.Background(), c, "/sections/"+args[0], q)
	},
}

func init() {
	sectionListCmd.Flags().StringVar(&sectionListProject, "project", "", "project gid (required)")
	sectionListCmd.Flags().BoolVar(&sectionListPaginate, "paginate", false, "fetch all pages")
	addFieldsFlag(sectionListCmd, &sectionListFields, "name")

	addFieldsFlag(sectionGetCmd, &sectionGetFields, "name")

	sectionCmd.AddCommand(sectionListCmd, sectionGetCmd)
}
