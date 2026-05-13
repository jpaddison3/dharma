package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Task commands",
}

var (
	taskListAssignee string
	taskListProject  string
	taskListLimit    int
	taskListFields   string
	taskListPaginate bool
)

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks (requires --project, or --assignee with --workspace)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		q := url.Values{}
		switch {
		case taskListProject != "":
			q.Set("project", taskListProject)
		case taskListAssignee != "":
			ws := resolveWorkspace()
			if ws == "" {
				return fmt.Errorf("--workspace required with --assignee")
			}
			q.Set("workspace", ws)
			q.Set("assignee", taskListAssignee)
		default:
			return fmt.Errorf("provide --project or --assignee")
		}
		if taskListLimit > 0 {
			q.Set("limit", strconv.Itoa(taskListLimit))
		}
		if taskListFields != "" {
			q.Set("opt_fields", taskListFields)
		}
		return runList(context.Background(), c, "/tasks", q, taskListPaginate)
	},
}

var taskGetFields string

var taskGetCmd = &cobra.Command{
	Use:   "get <gid>",
	Short: "Fetch a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		q := url.Values{}
		if taskGetFields != "" {
			q.Set("opt_fields", taskGetFields)
		}
		return runGet(context.Background(), c, "/tasks/"+args[0], q)
	},
}

var (
	taskCreateName     string
	taskCreateProjects []string
	taskCreateNotes    string
	taskCreateAssignee string
)

var taskCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a task",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		if taskCreateName == "" {
			return fmt.Errorf("--name is required")
		}
		body := map[string]interface{}{"name": taskCreateName}
		if len(taskCreateProjects) > 0 {
			body["projects"] = taskCreateProjects
		} else if ws := resolveWorkspace(); ws != "" {
			body["workspace"] = ws
		} else {
			return fmt.Errorf("provide --project or set a default workspace")
		}
		if taskCreateNotes != "" {
			body["notes"] = taskCreateNotes
		}
		if taskCreateAssignee != "" {
			body["assignee"] = taskCreateAssignee
		}
		return runPost(context.Background(), c, "/tasks", body)
	},
}

func init() {
	taskListCmd.Flags().StringVar(&taskListAssignee, "assignee", "", "assignee gid (use 'me' for self)")
	taskListCmd.Flags().StringVar(&taskListProject, "project", "", "project gid")
	taskListCmd.Flags().IntVar(&taskListLimit, "limit", 0, "max items per page (server default if 0)")
	taskListCmd.Flags().StringVar(&taskListFields, "fields", "", "opt_fields, e.g. name,assignee.name")
	taskListCmd.Flags().BoolVar(&taskListPaginate, "paginate", false, "fetch all pages")

	taskGetCmd.Flags().StringVar(&taskGetFields, "fields", "", "opt_fields, e.g. name,assignee.name")

	taskCreateCmd.Flags().StringVar(&taskCreateName, "name", "", "task name (required)")
	taskCreateCmd.Flags().StringArrayVar(&taskCreateProjects, "project", nil, "project gid (repeatable)")
	taskCreateCmd.Flags().StringVar(&taskCreateNotes, "notes", "", "task description")
	taskCreateCmd.Flags().StringVar(&taskCreateAssignee, "assignee", "", "assignee gid")

	taskCmd.AddCommand(taskListCmd, taskGetCmd, taskCreateCmd)
}
