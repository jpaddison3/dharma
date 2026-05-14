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
	taskListSection  string
	taskListLimit    int
	taskListFields   string
	taskListPaginate bool
)

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks (requires --section, --project, or --assignee with --workspace)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		q := url.Values{}
		switch {
		case taskListSection != "":
			q.Set("section", taskListSection)
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
			return fmt.Errorf("provide --section, --project, or --assignee")
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

var taskCommentText string

var taskCommentCmd = &cobra.Command{
	Use:   "comment <gid>",
	Short: "Add a comment (story) to a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if taskCommentText == "" {
			return fmt.Errorf("--text is required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPost(context.Background(), c, "/tasks/"+args[0]+"/stories", map[string]interface{}{"text": taskCommentText})
	},
}

var taskMoveSection string

var taskMoveCmd = &cobra.Command{
	Use:   "move <gid>",
	Short: "Move a task into a section (within whichever project that section belongs to)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if taskMoveSection == "" {
			return fmt.Errorf("--section is required (a section gid)")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPost(context.Background(), c, "/sections/"+taskMoveSection+"/addTask", map[string]interface{}{"task": args[0]})
	},
}

var (
	taskAddToProjectProject string
	taskAddToProjectSection string
)

var taskAddToProjectCmd = &cobra.Command{
	Use:   "add-to-project <gid>",
	Short: "Add a task to a project (multi-home), optionally placing it in a section",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if taskAddToProjectProject == "" {
			return fmt.Errorf("--project is required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		body := map[string]interface{}{"project": taskAddToProjectProject}
		if taskAddToProjectSection != "" {
			body["section"] = taskAddToProjectSection
		}
		return runPost(context.Background(), c, "/tasks/"+args[0]+"/addProject", body)
	},
}

var taskRemoveFromProjectProject string

var taskRemoveFromProjectCmd = &cobra.Command{
	Use:   "remove-from-project <gid>",
	Short: "Remove a task from a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if taskRemoveFromProjectProject == "" {
			return fmt.Errorf("--project is required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPost(context.Background(), c, "/tasks/"+args[0]+"/removeProject", map[string]interface{}{"project": taskRemoveFromProjectProject})
	},
}

var taskAddTagTag string

var taskAddTagCmd = &cobra.Command{
	Use:   "add-tag <gid>",
	Short: "Add a tag to a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if taskAddTagTag == "" {
			return fmt.Errorf("--tag is required (a tag gid)")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPost(context.Background(), c, "/tasks/"+args[0]+"/addTag", map[string]interface{}{"tag": taskAddTagTag})
	},
}

var taskRenameName string

var taskRenameCmd = &cobra.Command{
	Use:   "rename <gid>",
	Short: "Rename a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if taskRenameName == "" {
			return fmt.Errorf("--name is required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPut(context.Background(), c, "/tasks/"+args[0], map[string]interface{}{"name": taskRenameName})
	},
}

var taskCompleteCmd = &cobra.Command{
	Use:   "complete <gid>",
	Short: "Mark a task complete",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPut(context.Background(), c, "/tasks/"+args[0], map[string]interface{}{"completed": true})
	},
}

func init() {
	taskListCmd.Flags().StringVar(&taskListAssignee, "assignee", "", "assignee gid (use 'me' for self)")
	taskListCmd.Flags().StringVar(&taskListProject, "project", "", "project gid")
	taskListCmd.Flags().StringVar(&taskListSection, "section", "", "section gid")
	taskListCmd.Flags().IntVar(&taskListLimit, "limit", 0, "max items per page (server default if 0)")
	taskListCmd.Flags().StringVar(&taskListFields, "fields", "", "opt_fields, e.g. name,assignee.name")
	taskListCmd.Flags().BoolVar(&taskListPaginate, "paginate", false, "fetch all pages")

	taskGetCmd.Flags().StringVar(&taskGetFields, "fields", "", "opt_fields, e.g. name,assignee.name")

	taskCreateCmd.Flags().StringVar(&taskCreateName, "name", "", "task name (required)")
	taskCreateCmd.Flags().StringArrayVar(&taskCreateProjects, "project", nil, "project gid (repeatable)")
	taskCreateCmd.Flags().StringVar(&taskCreateNotes, "notes", "", "task description")
	taskCreateCmd.Flags().StringVar(&taskCreateAssignee, "assignee", "", "assignee gid")

	taskCommentCmd.Flags().StringVar(&taskCommentText, "text", "", "comment text (URLs are auto-linked by Asana)")

	taskMoveCmd.Flags().StringVar(&taskMoveSection, "section", "", "destination section gid")

	taskAddToProjectCmd.Flags().StringVar(&taskAddToProjectProject, "project", "", "project gid (required)")
	taskAddToProjectCmd.Flags().StringVar(&taskAddToProjectSection, "section", "", "section gid within the project (optional)")

	taskRemoveFromProjectCmd.Flags().StringVar(&taskRemoveFromProjectProject, "project", "", "project gid (required)")

	taskAddTagCmd.Flags().StringVar(&taskAddTagTag, "tag", "", "tag gid (required)")

	taskRenameCmd.Flags().StringVar(&taskRenameName, "name", "", "new name (required)")

	taskCmd.AddCommand(taskListCmd, taskGetCmd, taskCreateCmd, taskCommentCmd, taskMoveCmd, taskAddToProjectCmd, taskRemoveFromProjectCmd, taskAddTagCmd, taskRenameCmd, taskCompleteCmd)
}
