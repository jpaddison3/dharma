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

func init() {
	sectionListCmd.Flags().StringVar(&sectionListProject, "project", "", "project gid (required)")
	sectionListCmd.Flags().BoolVar(&sectionListPaginate, "paginate", false, "fetch all pages")
	addFieldsFlag(sectionListCmd, &sectionListFields, "name")
	sectionCmd.AddCommand(sectionListCmd)
}
