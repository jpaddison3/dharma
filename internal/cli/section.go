package cli

import (
	"context"

	"github.com/spf13/cobra"
)

var sectionCmd = &cobra.Command{
	Use:   "section",
	Short: "Section commands",
}

var (
	sectionListProject  string
	sectionListPaginate bool
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
		return runList(context.Background(), c, "/projects/"+sectionListProject+"/sections", nil, sectionListPaginate)
	},
}

func init() {
	sectionListCmd.Flags().StringVar(&sectionListProject, "project", "", "project gid (required)")
	sectionListCmd.Flags().BoolVar(&sectionListPaginate, "paginate", false, "fetch all pages")
	sectionCmd.AddCommand(sectionListCmd)
}
