package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/jpaddison3/dharma/internal/client"
	"github.com/spf13/cobra"
)

var myTasksCmd = &cobra.Command{
	Use:   "my-tasks",
	Short: "Your My Tasks list",
}

var (
	myTasksListSection  string
	myTasksListFields   string
	myTasksListLimit    int
	myTasksListPaginate bool
)

var myTasksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks in your My Tasks; --section filters to a named section like \"Main work\"",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		ws := resolveWorkspace()
		if ws == "" {
			return fmt.Errorf("workspace required (--workspace, ASANA_WORKSPACE, or config default)")
		}
		ctx := context.Background()

		q := url.Values{}
		if myTasksListFields != "" {
			q.Set("opt_fields", myTasksListFields)
		}
		if myTasksListLimit > 0 {
			q.Set("limit", strconv.Itoa(myTasksListLimit))
		}

		if myTasksListSection != "" {
			sectionGID, err := resolveMyTasksSection(ctx, c, ws, myTasksListSection)
			if err != nil {
				return err
			}
			q.Set("section", sectionGID)
			return runList(ctx, c, "/tasks", q, myTasksListPaginate)
		}

		q.Set("assignee", "me")
		q.Set("workspace", ws)
		return runList(ctx, c, "/tasks", q, myTasksListPaginate)
	},
}

// resolveMyTasksSection looks up the user's My Tasks user_task_list and finds a
// section by name. Returns the section gid.
func resolveMyTasksSection(ctx context.Context, c *client.Client, workspace, name string) (string, error) {
	var utl struct {
		GID string `json:"gid"`
	}
	if err := c.Get(ctx, "/users/me/user_task_list", url.Values{"workspace": []string{workspace}}, &utl); err != nil {
		return "", fmt.Errorf("fetching user_task_list: %w", err)
	}
	if utl.GID == "" {
		return "", fmt.Errorf("no user_task_list returned for workspace %s", workspace)
	}

	var sections []struct {
		GID  string `json:"gid"`
		Name string `json:"name"`
	}
	if err := c.Get(ctx, "/projects/"+utl.GID+"/sections", nil, &sections); err != nil {
		return "", fmt.Errorf("listing My Tasks sections: %w", err)
	}

	var found string
	multiple := false
	for _, s := range sections {
		if s.Name == name {
			if found != "" {
				multiple = true
			}
			if found == "" {
				found = s.GID
			}
		}
	}
	if found == "" {
		names := make([]string, 0, len(sections))
		for _, s := range sections {
			names = append(names, fmt.Sprintf("%q", s.Name))
		}
		return "", fmt.Errorf("section %q not found in My Tasks. Available: %s", name, strings.Join(names, ", "))
	}
	if multiple {
		fmt.Fprintf(os.Stderr, "warning: multiple sections named %q in My Tasks; using first\n", name)
	}
	return found, nil
}

func init() {
	myTasksListCmd.Flags().StringVar(&myTasksListSection, "section", "", "section name (e.g. \"Main work\"); omit for all My Tasks")
	myTasksListCmd.Flags().StringVar(&myTasksListFields, "fields", "", "opt_fields, e.g. name,assignee.name,due_on")
	myTasksListCmd.Flags().IntVar(&myTasksListLimit, "limit", 0, "max items per page (server default if 0)")
	myTasksListCmd.Flags().BoolVar(&myTasksListPaginate, "paginate", false, "fetch all pages")
	myTasksCmd.AddCommand(myTasksListCmd)
}
