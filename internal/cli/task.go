package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jpaddison3/dharma/internal/client"
	"github.com/jpaddison3/dharma/internal/output"
	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Task commands",
}

// Curated default opt_fields. Passing them keeps payloads small AND strips
// Asana's resource_type noise (present only when opt_fields is omitted). Pass
// --fields "" to fall back to Asana's raw default representation.
const (
	defaultTaskListFields = "name,completed,due_on,assignee.name"
	defaultTaskGetFields  = "name,notes,completed,due_on,assignee.name,projects.name,parent.name,num_subtasks,attachments.name,tags.name,permalink_url,created_at,modified_at"
)

var (
	taskListAssignee   string
	taskListProject    string
	taskListSection    string
	taskListLimit      int
	taskListFields     string
	taskListPaginate   bool
	taskListIncomplete bool
)

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks (requires --section, --project, or --assignee)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx := context.Background()
		q := url.Values{}
		switch {
		case taskListSection != "":
			q.Set("section", taskListSection)
		case taskListProject != "":
			q.Set("project", taskListProject)
		case taskListAssignee != "":
			ws, err := requireWorkspace(ctx, c)
			if err != nil {
				return err
			}
			q.Set("workspace", ws)
			q.Set("assignee", taskListAssignee)
		default:
			return usageErrorf("provide --section, --project, or --assignee")
		}
		if taskListLimit > 0 {
			q.Set("limit", strconv.Itoa(taskListLimit))
		}
		setOptFields(q, taskListFields)
		if taskListIncomplete {
			q.Set("completed_since", "now")
		}
		return runList(ctx, c, "/tasks", q, taskListPaginate)
	},
}

var (
	taskGetFields    string
	taskGetNoContext bool
	taskGetFull      bool
)

var taskGetCmd = &cobra.Command{
	Use:   "get <gid>",
	Short: "Fetch a task, with a context block summarizing comments/attachments/subtasks",
	Long: `Fetch a task. By default the response carries a sibling "context" block that
summarizes things an agent might not think to look for — the comment count
(one extra stories call, run in parallel), plus attachment names, subtask
count, and project names pulled free from the task itself. Use --no-context to
skip the extra call, or --fields to change what the task object returns (the
context reflects whatever fields come back).

Long notes are truncated with an inline marker (and named in truncated_fields);
pass --full to get the untruncated text.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		gid := args[0]
		ctx := context.Background()
		q := url.Values{}
		setOptFields(q, taskGetFields)

		var task map[string]interface{}
		var contextBlock interface{}

		if taskGetNoContext {
			if err := c.Get(ctx, "/tasks/"+gid, q, &task); err != nil {
				return err
			}
		} else {
			// Count comments in parallel with the main fetch. A buffered channel
			// means the goroutine never leaks even if the main fetch errors first.
			type storiesResult struct {
				count int
				err   error
			}
			ch := make(chan storiesResult, 1)
			go func() {
				n, err := countComments(ctx, c, gid)
				ch <- storiesResult{n, err}
			}()

			if err := c.Get(ctx, "/tasks/"+gid, q, &task); err != nil {
				return err
			}

			sr := <-ch
			var commentCount *int
			if sr.err != nil {
				// Degrade: the task itself is fine, so don't fail the command —
				// note the miss on stderr and leave comments null.
				fmt.Fprintf(os.Stderr, "warning: could not count comments: %v\n", sr.err)
			} else {
				commentCount = &sr.count
			}
			contextBlock = buildTaskContext(gid, task, commentCount)
		}

		var truncatedFields []string
		if !taskGetFull {
			if notes, ok := task["notes"].(string); ok {
				if shortened, was := truncateText(notes); was {
					task["notes"] = shortened
					truncatedFields = append(truncatedFields, "notes")
				}
			}
		}
		return output.PrintObjectFull(os.Stdout, task, contextBlock, truncatedFields)
	},
}

// truncateLimit caps long free-text fields (notes, story text) at this many
// runes before an inline marker is appended. Tunable; chosen to keep a task or
// comment readable without dumping a whole document into context.
const truncateLimit = 2000

// truncateText shortens s to truncateLimit runes (UTF-8 safe, never splitting a
// multibyte rune) with an inline marker naming the full length. Returns the
// possibly-shortened string and whether it changed.
func truncateText(s string) (string, bool) {
	// Bytes >= runes, so a short byte length rules out truncation cheaply.
	if len(s) <= truncateLimit {
		return s, false
	}
	runes := []rune(s)
	if len(runes) <= truncateLimit {
		return s, false
	}
	return fmt.Sprintf("%s… (truncated, %d chars total — rerun with --full)", string(runes[:truncateLimit]), len(runes)), true
}

// countComments tallies comment_added stories on a task, requesting only
// resource_subtype to keep each page tiny. Pages fully (capped well above any
// realistic task) so recent comments on later pages aren't missed.
func countComments(ctx context.Context, c *client.Client, gid string) (int, error) {
	q := url.Values{}
	q.Set("opt_fields", "resource_subtype")
	q.Set("limit", "100")
	const maxPages = 10
	count := 0
	for page := 0; page < maxPages; page++ {
		resp, err := c.Do(ctx, "GET", "/tasks/"+gid+"/stories", q, nil)
		if err != nil {
			return 0, err
		}
		var stories []struct {
			ResourceSubtype string `json:"resource_subtype"`
		}
		if err := json.Unmarshal(resp.Data, &stories); err != nil {
			return 0, err
		}
		for _, s := range stories {
			if s.ResourceSubtype == "comment_added" {
				count++
			}
		}
		if resp.NextPage == nil || resp.NextPage.Offset == "" {
			break
		}
		q.Set("offset", resp.NextPage.Offset)
	}
	return count, nil
}

// buildTaskContext assembles the context block from the fetched task plus the
// (possibly nil) comment count. Fields absent from the task object — because
// --fields narrowed them out — are omitted rather than reported as empty, so
// the block never claims "no attachments" when it simply didn't ask.
func buildTaskContext(gid string, task map[string]interface{}, commentCount *int) map[string]interface{} {
	block := map[string]interface{}{}
	if commentCount != nil {
		block["comments"] = *commentCount
	} else {
		block["comments"] = nil // stories call failed
	}
	if v, present := task["attachments"]; present {
		block["attachments"] = extractNames(v)
	}
	if v, present := task["num_subtasks"]; present {
		if n, ok := v.(float64); ok {
			block["subtasks"] = int(n)
		}
	}
	if v, present := task["projects"]; present {
		block["projects"] = extractNames(v)
	}
	if commentCount != nil && *commentCount > 0 {
		block["hint"] = fmt.Sprintf("dharma task stories %s — read the %d comment(s)", gid, *commentCount)
	}
	return block
}

// extractNames pulls the "name" of each object in a JSON array (decoded as
// []interface{} of map[string]interface{}), returning a non-nil slice.
func extractNames(v interface{}) []string {
	names := []string{}
	arr, ok := v.([]interface{})
	if !ok {
		return names
	}
	for _, item := range arr {
		if m, ok := item.(map[string]interface{}); ok {
			if name, ok := m["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names
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
			return usageErrorf("--name is required")
		}
		body := map[string]interface{}{"name": taskCreateName}
		if len(taskCreateProjects) > 0 {
			body["projects"] = taskCreateProjects
		} else {
			ws, err := requireWorkspace(context.Background(), c)
			if err != nil {
				return err
			}
			body["workspace"] = ws
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
			return usageErrorf("--text is required")
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
			return usageErrorf("--section is required (a section gid)")
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
			return usageErrorf("--project is required")
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
			return usageErrorf("--project is required")
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
			return usageErrorf("--tag is required (a tag gid)")
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
			return usageErrorf("--name is required")
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

var (
	taskSetDueDate  string
	taskSetDueClear bool
)

var taskSetDueCmd = &cobra.Command{
	Use:   "set-due <gid>",
	Short: "Set or clear a task's due date",
	Long:  "Set or clear a task's due date. --due accepts YYYY-MM-DD, 'today', 'tomorrow', or an ISO 8601 datetime (containing 'T'). Use --clear to remove the due date.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if taskSetDueClear && taskSetDueDate != "" {
			return usageErrorf("--clear and --due are mutually exclusive")
		}
		if !taskSetDueClear && taskSetDueDate == "" {
			return usageErrorf("--due is required (YYYY-MM-DD, today, tomorrow, or ISO datetime), or use --clear")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		body := map[string]interface{}{}
		if taskSetDueClear {
			body["due_on"] = nil
		} else {
			value := taskSetDueDate
			switch strings.ToLower(value) {
			case "today":
				value = time.Now().Format("2006-01-02")
			case "tomorrow":
				value = time.Now().AddDate(0, 0, 1).Format("2006-01-02")
			}
			if strings.Contains(value, "T") {
				body["due_at"] = value
			} else {
				body["due_on"] = value
			}
		}
		return runPut(context.Background(), c, "/tasks/"+args[0], body)
	},
}

var (
	taskAssignTo    string
	taskAssignClear bool
)

var taskAssignCmd = &cobra.Command{
	Use:   "assign <gid>",
	Short: "Assign, reassign, or unassign a task",
	Long:  "Set or clear a task's assignee. --to accepts a user gid or the literal 'me'. Use --clear to unassign.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if taskAssignClear && taskAssignTo != "" {
			return usageErrorf("--clear and --to are mutually exclusive")
		}
		if !taskAssignClear && taskAssignTo == "" {
			return usageErrorf("--to is required (user gid or 'me'), or use --clear")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		body := map[string]interface{}{}
		if taskAssignClear {
			body["assignee"] = nil
		} else {
			body["assignee"] = taskAssignTo
		}
		return runPut(context.Background(), c, "/tasks/"+args[0], body)
	},
}

var taskSetNotesText string

var taskSetNotesCmd = &cobra.Command{
	Use:   "set-notes <gid>",
	Short: "Set a task's description (notes)",
	Long:  "Set a task's description. Pass --notes \"\" to clear.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("notes") {
			return usageErrorf("--notes is required (pass \"\" to clear)")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPut(context.Background(), c, "/tasks/"+args[0], map[string]interface{}{"notes": taskSetNotesText})
	},
}

var (
	taskSearchText          string
	taskSearchAssignee      string
	taskSearchCompleted     bool
	taskSearchProject       string
	taskSearchSection       string
	taskSearchTag           string
	taskSearchModifiedSince string
	taskSearchFields        string
	taskSearchLimit         int
)

var taskSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search tasks across a workspace",
	Long: `Search tasks across a workspace using Asana's /tasks/search endpoint.

The endpoint returns at most 100 results in a single response and does NOT support
offset pagination. To page through a longer result set, narrow with the provided
filters or chunk by modification time: take the oldest result's modified_at value
and re-run with --modified-since pointing earlier (Asana's parameter is actually
modified_at.after, so this fetches anything newer than that timestamp — invert
manually if you need older).

A warning is printed to stderr if the result count equals --limit, since results
may have been truncated.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		ws, err := requireWorkspace(context.Background(), c)
		if err != nil {
			return err
		}
		limit := taskSearchLimit
		if limit <= 0 {
			limit = 100
		}
		q := url.Values{}
		q.Set("limit", strconv.Itoa(limit))
		if taskSearchText != "" {
			q.Set("text", taskSearchText)
		}
		if taskSearchAssignee != "" {
			q.Set("assignee.any", taskSearchAssignee)
		}
		if cmd.Flags().Changed("completed") {
			q.Set("completed", strconv.FormatBool(taskSearchCompleted))
		}
		if taskSearchProject != "" {
			q.Set("projects.any", taskSearchProject)
		}
		if taskSearchSection != "" {
			q.Set("sections.any", taskSearchSection)
		}
		if taskSearchTag != "" {
			q.Set("tags.any", taskSearchTag)
		}
		if taskSearchModifiedSince != "" {
			q.Set("modified_at.after", taskSearchModifiedSince)
		}
		setOptFields(q, taskSearchFields)
		resp, err := c.Do(context.Background(), "GET", "/workspaces/"+ws+"/tasks/search", q, nil)
		if err != nil {
			return err
		}
		var results []interface{}
		if err := json.Unmarshal(resp.Data, &results); err != nil {
			return fmt.Errorf("search response was not an array: %w", err)
		}
		hasMore := len(results) >= limit
		hint := ""
		if hasMore {
			// The search endpoint has no offset pagination, so a full page
			// likely means truncation. Point at the one workaround.
			hint = fmt.Sprintf("hit the %d-result cap and search has no pagination — narrow filters, or take the oldest result's modified_at and rerun with --modified-since <that timestamp> to page by modification time", limit)
		}
		return output.PrintList(os.Stdout, results, hasMore, hint)
	},
}

var (
	taskStoriesFields   string
	taskStoriesPaginate bool
	taskStoriesFull     bool
)

var taskStoriesCmd = &cobra.Command{
	Use:   "stories <gid>",
	Short: "List stories (comments and change log) on a task",
	Long: `List stories (comments and change log) on a task. Long comment text is
truncated with an inline marker (and "text" listed in truncated_fields); pass
--full for untruncated text.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		q := url.Values{}
		setOptFields(q, taskStoriesFields)
		all, hasMore, err := fetchList(context.Background(), c, "/tasks/"+args[0]+"/stories", q, taskStoriesPaginate)
		if err != nil {
			return err
		}
		var truncatedFields []string
		if !taskStoriesFull {
			truncatedAny := false
			for _, item := range all {
				m, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				if text, ok := m["text"].(string); ok {
					if shortened, was := truncateText(text); was {
						m["text"] = shortened
						truncatedAny = true
					}
				}
			}
			if truncatedAny {
				truncatedFields = []string{"text"}
			}
		}
		return output.PrintListFull(os.Stdout, all, hasMore, paginateHintFor(hasMore), truncatedFields)
	},
}

func init() {
	taskListCmd.Flags().StringVar(&taskListAssignee, "assignee", "", "assignee gid (use 'me' for self)")
	taskListCmd.Flags().StringVar(&taskListProject, "project", "", "project gid")
	taskListCmd.Flags().StringVar(&taskListSection, "section", "", "section gid")
	taskListCmd.Flags().IntVar(&taskListLimit, "limit", 0, "max items per page (server default if 0)")
	addFieldsFlag(taskListCmd, &taskListFields, defaultTaskListFields)
	taskListCmd.Flags().BoolVar(&taskListPaginate, "paginate", false, "fetch all pages")
	taskListCmd.Flags().BoolVar(&taskListIncomplete, "incomplete", false, "only tasks not yet completed (completed_since=now)")

	addFieldsFlag(taskGetCmd, &taskGetFields, defaultTaskGetFields)
	taskGetCmd.Flags().BoolVar(&taskGetNoContext, "no-context", false, "skip the context block (avoids the extra comment-count call)")
	taskGetCmd.Flags().BoolVar(&taskGetFull, "full", false, "return full notes without truncation")

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

	taskSetDueCmd.Flags().StringVar(&taskSetDueDate, "due", "", "YYYY-MM-DD, today, tomorrow, or ISO 8601 datetime")
	taskSetDueCmd.Flags().BoolVar(&taskSetDueClear, "clear", false, "clear the due date")

	taskAssignCmd.Flags().StringVar(&taskAssignTo, "to", "", "assignee user gid, or 'me'")
	taskAssignCmd.Flags().BoolVar(&taskAssignClear, "clear", false, "unassign the task")

	taskSetNotesCmd.Flags().StringVar(&taskSetNotesText, "notes", "", "new description (pass \"\" to clear)")

	taskSearchCmd.Flags().StringVar(&taskSearchText, "text", "", "match name/description")
	taskSearchCmd.Flags().StringVar(&taskSearchAssignee, "assignee", "", "user gid or 'me'")
	taskSearchCmd.Flags().BoolVar(&taskSearchCompleted, "completed", false, "filter by completion (omit to return either)")
	taskSearchCmd.Flags().StringVar(&taskSearchProject, "project", "", "project gid")
	taskSearchCmd.Flags().StringVar(&taskSearchSection, "section", "", "section gid")
	taskSearchCmd.Flags().StringVar(&taskSearchTag, "tag", "", "tag gid")
	taskSearchCmd.Flags().StringVar(&taskSearchModifiedSince, "modified-since", "", "ISO 8601 datetime; maps to modified_at.after")
	addFieldsFlag(taskSearchCmd, &taskSearchFields, defaultTaskListFields)
	taskSearchCmd.Flags().IntVar(&taskSearchLimit, "limit", 0, "max results (1-100, default 100)")

	addFieldsFlag(taskStoriesCmd, &taskStoriesFields, "type,text,created_at,created_by.name")
	taskStoriesCmd.Flags().BoolVar(&taskStoriesPaginate, "paginate", false, "fetch all pages")
	taskStoriesCmd.Flags().BoolVar(&taskStoriesFull, "full", false, "return full comment text without truncation")

	taskCmd.AddCommand(taskListCmd, taskGetCmd, taskCreateCmd, taskCommentCmd, taskMoveCmd, taskAddToProjectCmd, taskRemoveFromProjectCmd, taskAddTagCmd, taskRenameCmd, taskCompleteCmd, taskSetDueCmd, taskAssignCmd, taskSetNotesCmd, taskSearchCmd, taskStoriesCmd)
}
