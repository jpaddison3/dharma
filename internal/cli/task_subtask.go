package cli

import (
	"context"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

var taskSubtaskCmd = &cobra.Command{
	Use:   "subtask",
	Short: "List and create direct subtasks",
}

var (
	taskSubtaskListFields   string
	taskSubtaskListLimit    int
	taskSubtaskListPaginate bool
)

var taskSubtaskListCmd = &cobra.Command{
	Use:   "list <parent-gid>",
	Short: "List a task's direct subtasks",
	Long: `List the direct children of a task, including completed subtasks, in
Asana's API order. By default only the first page is fetched; use --paginate
to follow all pages.`,
	Example: `  dharma task subtask list 1234567890
  dharma task subtask list 1234567890 --paginate --limit 100
  dharma task subtask list 1234567890 --fields 'name,parent.gid,assignee.name'`,
	Args: exactNonEmptyGID("parent-gid"),
	RunE: func(cmd *cobra.Command, args []string) error {
		if taskSubtaskListLimit < 1 || taskSubtaskListLimit > 100 {
			return usageErrorf("--limit must be between 1 and 100")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		q := url.Values{"limit": []string{strconv.Itoa(taskSubtaskListLimit)}}
		setOptFields(q, taskSubtaskListFields)
		return runList(context.Background(), c, "/tasks/"+args[0]+"/subtasks", q, taskSubtaskListPaginate)
	},
}

var (
	taskSubtaskCreateName      string
	taskSubtaskCreateProjects  []string
	taskSubtaskCreateNotes     string
	taskSubtaskCreateHTMLNotes string
	taskSubtaskCreateAssignee  string
)

var taskSubtaskCreateCmd = &cobra.Command{
	Use:   "create <parent-gid>",
	Short: "Create a direct subtask",
	Long: `Create a direct child of a task without resolving a workspace or
inheriting the parent's projects or assignee. Repeat --project to explicitly
multi-home the new subtask.

Use --notes for literal plain text, or --html-notes for Asana rich text; the
flags are mutually exclusive. Rich text accepts literal markup, @path, or @-
for stdin. File/stdin input has exactly one final LF removed and must be a
non-empty, balanced XML fragment wrapped in <body>...</body>. Use <body></body>
for an empty formatted description. See dharma api --help for supported markup
and mention links.`,
	Example: `  dharma task subtask create 1234567890 --name "Investigate"
  dharma task subtask create 1234567890 --name "Draft" --assignee me --project 9876543210
  dharma task subtask create 1234567890 --name "Formatted" --html-notes @description.html`,
	Args: exactNonEmptyGID("parent-gid"),
	RunE: func(cmd *cobra.Command, args []string) error {
		if taskSubtaskCreateName == "" {
			return usageErrorf("--name is required")
		}
		body, err := buildTaskCreateBody(
			taskSubtaskCreateName, taskSubtaskCreateNotes, taskSubtaskCreateHTMLNotes, taskSubtaskCreateAssignee,
			cmd.Flags().Changed("notes"), cmd.Flags().Changed("html-notes"),
			cmd.ErrOrStderr(),
		)
		if err != nil {
			return err
		}
		if len(taskSubtaskCreateProjects) > 0 {
			body["projects"] = taskSubtaskCreateProjects
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPost(context.Background(), c, "/tasks/"+args[0]+"/subtasks", body)
	},
}

var (
	taskSetParentParent string
	taskSetParentClear  bool
)

var taskSetParentCmd = &cobra.Command{
	Use:   "set-parent <task-gid>",
	Short: "Change or clear a task's parent",
	Long: `Make a task a direct child of --parent, or use --clear to make it a
top-level task. Asana validates hierarchy rules and permissions.`,
	Example: `  dharma task set-parent 1234567890 --parent 9876543210
  dharma task set-parent 1234567890 --clear`,
	Args: exactNonEmptyGID("task-gid"),
	RunE: func(cmd *cobra.Command, args []string) error {
		parentPresent := cmd.Flags().Changed("parent")
		clearPresent := cmd.Flags().Changed("clear")
		if parentPresent && clearPresent {
			return usageErrorf("--parent and --clear are mutually exclusive")
		}

		body := map[string]interface{}{}
		switch {
		case parentPresent:
			if taskSetParentParent == "" {
				return usageErrorf("--parent must not be empty")
			}
			body["parent"] = taskSetParentParent
		case taskSetParentClear:
			body["parent"] = nil
		default:
			return usageErrorf("--parent is required, or use --clear")
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		return runPost(context.Background(), c, "/tasks/"+args[0]+"/setParent", body)
	},
}

func exactNonEmptyGID(name string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}
		if args[0] == "" {
			return usageErrorf("%s must not be empty", name)
		}
		return nil
	}
}

func init() {
	addFieldsFlag(taskSubtaskListCmd, &taskSubtaskListFields, defaultTaskListFields)
	taskSubtaskListCmd.Flags().BoolVar(&taskSubtaskListPaginate, "paginate", false, "fetch all pages")
	taskSubtaskListCmd.Flags().IntVar(&taskSubtaskListLimit, "limit", 100, "max items per page (1-100)")

	taskSubtaskCreateCmd.Flags().StringVar(&taskSubtaskCreateName, "name", "", "task name (required)")
	taskSubtaskCreateCmd.Flags().StringArrayVar(&taskSubtaskCreateProjects, "project", nil, "project gid (repeatable)")
	taskSubtaskCreateCmd.Flags().StringVar(&taskSubtaskCreateNotes, "notes", "", "literal plain-text task description (mutually exclusive with --html-notes)")
	taskSubtaskCreateCmd.Flags().StringVar(&taskSubtaskCreateHTMLNotes, "html-notes", "", "Asana rich-text description: literal HTML, @path, or @- (mutually exclusive with --notes)")
	taskSubtaskCreateCmd.Flags().StringVar(&taskSubtaskCreateAssignee, "assignee", "", "assignee gid or 'me'")

	taskSetParentCmd.Flags().StringVar(&taskSetParentParent, "parent", "", "new parent task gid")
	taskSetParentCmd.Flags().BoolVar(&taskSetParentClear, "clear", false, "remove the task's parent")

	taskSubtaskCmd.AddCommand(taskSubtaskListCmd, taskSubtaskCreateCmd)
	taskCmd.AddCommand(taskSubtaskCmd, taskSetParentCmd)
}
