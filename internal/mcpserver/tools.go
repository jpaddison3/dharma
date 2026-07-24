package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerTools adds the 12 tools ported from mcpb/server/index.js: same
// names, descriptions, schemas (inferred from the arg structs below via
// jsonschema tags), and argv builders. Argv builders place "--" before
// model-supplied positionals so a value starting with "-" can't be parsed as
// a flag.
func (s *server) registerTools(mcpServer *mcp.Server) error {
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "whoami",
		Description: "Get the authenticated Asana user (gid, name, email). Useful as a connectivity check.",
	}, s.whoami)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "my_tasks",
		Description: `List open tasks in the user's My Tasks. Returns {ok,count,has_more,data:[...]}; has_more=true means more than the first page exist (set paginate). Optionally filter to a named section (e.g. "Main Work").`,
	}, s.myTasks)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "search_tasks",
		Description: "Search tasks across the workspace by text and filters. Returns {ok,count,has_more,data:[...]} with at most 100 results; has_more=true means the cap was hit — narrow filters (the result's hint field suggests how).",
	}, s.searchTasks)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "get_task",
		Description: `Get a single task by gid. Returns {ok,data,context}, where context summarizes the comment count, attachment names, subtask count, and project names — read it to decide what to follow up on (e.g. task_stories when comments > 0) without extra calls. On very busy tasks the comment count is a string like "80+" rather than an exact number.`,
	}, s.getTask)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "task_stories",
		Description: "Get a task's stories (comments and activity history). Fields default to type,text,created_at,created_by.name. Long comment text is truncated (see truncated_fields); pass full for complete text.",
	}, s.taskStories)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "list_projects",
		Description: "List projects in the workspace. Returns {ok,count,has_more,data:[...]}; has_more=true means more than the first page exist (set paginate).",
	}, s.listProjects)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "list_project_tasks",
		Description: "List open tasks in a project. Returns {ok,count,has_more,data:[...]}; has_more=true means more than the first page exist (set paginate).",
	}, s.listProjectTasks)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "create_task",
		Description: "Create a task. With no project it goes to the user's My Tasks.",
	}, s.createTask)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "comment_task",
		Description: "Add a comment to a task.",
	}, s.commentTask)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "complete_task",
		Description: "Mark a task complete.",
	}, s.completeTask)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "set_due_date",
		Description: "Set or clear a task's due date.",
	}, s.setDueDate)

	apiSchema, err := asanaAPISchema()
	if err != nil {
		return err
	}
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "asana_api",
		Description: "Raw Asana API passthrough for anything the other tools don't cover (modeled on `gh api`). " +
			"field entries like key=value become query parameters on GET/DELETE and JSON body fields " +
			"(wrapped in Asana's {data: ...} envelope) on POST/PUT/PATCH.",
		InputSchema: apiSchema,
	}, s.asanaAPI)
	return nil
}

// httpMethods is the method allowlist, matching index.js:355's zod enum. It is
// the only allowlist in the stack: `dharma api -X` upper-cases whatever it is
// given and only branches on GET/DELETE/HEAD for body handling, so an unknown
// method reaches Asana with -f entries silently reclassified as body fields.
// Removing this enum removes the check entirely.
var httpMethods = []any{"GET", "POST", "PUT", "PATCH", "DELETE"}

// asanaAPISchema is the inferred asanaAPIArgs schema with the two things a Go
// struct tag can't express restored, so this tool constrains input the way
// index.js:355-357 does. (Its `method` description is deliberately wordier
// than Node's bare "HTTP method" — it names the allowed values for hosts that
// don't surface enums; everything else across the 12 tools is verbatim.)
//
//   - method's enum and default. The `jsonschema` tag only ever sets a
//     description (jsonschema-go infer.go), so without this the model can send
//     "FETCH", pass validation, and have the CLI silently reclassify -f
//     entries from query params to body fields.
//   - field's description, which begins with "key=value" — a leading WORD= is
//     reserved tag syntax and fails inference outright.
func asanaAPISchema() (*jsonschema.Schema, error) {
	schema, err := jsonschema.For[asanaAPIArgs](nil)
	if err != nil {
		return nil, fmt.Errorf("inferring asana_api schema: %w", err)
	}
	method, ok := schema.Properties["method"]
	if !ok {
		return nil, errors.New("inferring asana_api schema: no method property")
	}
	method.Enum = httpMethods
	method.Default = json.RawMessage(`"GET"`)

	field, ok := schema.Properties["field"]
	if !ok {
		return nil, errors.New("inferring asana_api schema: no field property")
	}
	field.Description = "key=value pairs (query params on GET/DELETE, body fields otherwise)"
	return schema, nil
}

type whoamiArgs struct{}

func (s *server) whoami(ctx context.Context, req *mcp.CallToolRequest, in whoamiArgs) (*mcp.CallToolResult, any, error) {
	return asResult(s.runDharma(ctx, noWorkspace, "user", "me")), nil, nil
}

type myTasksArgs struct {
	Section          string `json:"section,omitempty" jsonschema:"My Tasks section name to filter to"`
	Paginate         bool   `json:"paginate,omitempty" jsonschema:"Fetch all pages instead of the first 100 (can be large)"`
	IncludeCompleted bool   `json:"include_completed,omitempty" jsonschema:"Include completed tasks (first 100 in My Tasks order; default false)"`
	Fields           string `json:"fields,omitempty" jsonschema:"Comma-separated opt_fields, e.g. name,assignee.name,due_on"`
}

func (s *server) myTasks(ctx context.Context, req *mcp.CallToolRequest, in myTasksArgs) (*mcp.CallToolResult, any, error) {
	ws, err := s.resolveWorkspace(ctx)
	if err != nil {
		return nil, nil, err
	}
	argv := []string{"my-tasks", "list"}
	if !in.IncludeCompleted {
		argv = append(argv, "--incomplete")
	}
	if in.Paginate {
		argv = append(argv, "--paginate")
	} else {
		argv = append(argv, "--limit", "100")
	}
	if in.Section != "" {
		argv = append(argv, "--section", in.Section)
	}
	if in.Fields != "" {
		argv = append(argv, "--fields", in.Fields)
	}
	return asResult(s.runDharma(ctx, ws, argv...)), nil, nil
}

type searchTasksArgs struct {
	Text      string `json:"text,omitempty" jsonschema:"Match against task name/description"`
	Assignee  string `json:"assignee,omitempty" jsonschema:"Assignee user gid, or 'me'"`
	Project   string `json:"project,omitempty" jsonschema:"Project gid"`
	Completed *bool  `json:"completed,omitempty" jsonschema:"Filter by completion; omit for both"`
	Fields    string `json:"fields,omitempty" jsonschema:"Comma-separated opt_fields, e.g. name,assignee.name,due_on"`
}

func (s *server) searchTasks(ctx context.Context, req *mcp.CallToolRequest, in searchTasksArgs) (*mcp.CallToolResult, any, error) {
	ws, err := s.resolveWorkspace(ctx)
	if err != nil {
		return nil, nil, err
	}
	argv := []string{"task", "search"}
	if in.Text != "" {
		argv = append(argv, "--text", in.Text)
	}
	if in.Assignee != "" {
		argv = append(argv, "--assignee", in.Assignee)
	}
	if in.Project != "" {
		argv = append(argv, "--project", in.Project)
	}
	if in.Completed != nil {
		argv = append(argv, fmt.Sprintf("--completed=%t", *in.Completed))
	}
	if in.Fields != "" {
		argv = append(argv, "--fields", in.Fields)
	}
	return asResult(s.runDharma(ctx, ws, argv...)), nil, nil
}

type getTaskArgs struct {
	TaskGID string `json:"task_gid" jsonschema:"Task gid"`
	Fields  string `json:"fields,omitempty" jsonschema:"Comma-separated opt_fields, e.g. name,assignee.name,due_on"`
	Full    bool   `json:"full,omitempty" jsonschema:"Return full notes without truncation"`
}

func (s *server) getTask(ctx context.Context, req *mcp.CallToolRequest, in getTaskArgs) (*mcp.CallToolResult, any, error) {
	argv := []string{"task", "get"}
	if in.Fields != "" {
		argv = append(argv, "--fields", in.Fields)
	}
	if in.Full {
		argv = append(argv, "--full")
	}
	argv = append(argv, "--", in.TaskGID)
	return asResult(s.runDharma(ctx, noWorkspace, argv...)), nil, nil
}

type taskStoriesArgs struct {
	TaskGID string `json:"task_gid" jsonschema:"Task gid"`
	Fields  string `json:"fields,omitempty" jsonschema:"Comma-separated opt_fields, e.g. name,assignee.name,due_on"`
	Full    bool   `json:"full,omitempty" jsonschema:"Return full comment text without truncation"`
}

func (s *server) taskStories(ctx context.Context, req *mcp.CallToolRequest, in taskStoriesArgs) (*mcp.CallToolResult, any, error) {
	argv := []string{"task", "stories"}
	if in.Fields != "" {
		argv = append(argv, "--fields", in.Fields)
	}
	if in.Full {
		argv = append(argv, "--full")
	}
	argv = append(argv, "--", in.TaskGID)
	return asResult(s.runDharma(ctx, noWorkspace, argv...)), nil, nil
}

type listProjectsArgs struct {
	Paginate bool `json:"paginate,omitempty" jsonschema:"Fetch all pages instead of the first 100"`
}

func (s *server) listProjects(ctx context.Context, req *mcp.CallToolRequest, in listProjectsArgs) (*mcp.CallToolResult, any, error) {
	ws, err := s.resolveWorkspace(ctx)
	if err != nil {
		return nil, nil, err
	}
	argv := []string{"project", "list"}
	if in.Paginate {
		argv = append(argv, "--paginate")
	}
	return asResult(s.runDharma(ctx, ws, argv...)), nil, nil
}

type listProjectTasksArgs struct {
	ProjectGID       string `json:"project_gid" jsonschema:"Project gid"`
	IncludeCompleted bool   `json:"include_completed,omitempty" jsonschema:"Include completed tasks (default false)"`
	Paginate         bool   `json:"paginate,omitempty" jsonschema:"Fetch all pages instead of the first 100"`
	Fields           string `json:"fields,omitempty" jsonschema:"Comma-separated opt_fields, e.g. name,assignee.name,due_on"`
}

func (s *server) listProjectTasks(ctx context.Context, req *mcp.CallToolRequest, in listProjectTasksArgs) (*mcp.CallToolResult, any, error) {
	argv := []string{"task", "list", "--project", in.ProjectGID}
	if !in.IncludeCompleted {
		argv = append(argv, "--incomplete")
	}
	if in.Paginate {
		argv = append(argv, "--paginate")
	}
	if in.Fields != "" {
		argv = append(argv, "--fields", in.Fields)
	}
	return asResult(s.runDharma(ctx, noWorkspace, argv...)), nil, nil
}

type createTaskArgs struct {
	Name       string `json:"name" jsonschema:"Task name"`
	Notes      string `json:"notes,omitempty" jsonschema:"Task description"`
	ProjectGID string `json:"project_gid,omitempty" jsonschema:"Project to add the task to; omit to create in My Tasks"`
	Assignee   string `json:"assignee,omitempty" jsonschema:"Assignee user gid, or 'me'"`
}

func (s *server) createTask(ctx context.Context, req *mcp.CallToolRequest, in createTaskArgs) (*mcp.CallToolResult, any, error) {
	// A project-backed create infers its workspace from the project; only
	// workspace-level creates need resolution.
	ws := noWorkspace
	if in.ProjectGID == "" {
		var err error
		if ws, err = s.resolveWorkspace(ctx); err != nil {
			return nil, nil, err
		}
	}
	argv := []string{"task", "create", "--name", in.Name}
	if in.Notes != "" {
		argv = append(argv, "--notes", in.Notes)
	}
	if in.ProjectGID != "" {
		argv = append(argv, "--project", in.ProjectGID)
	}
	if in.Assignee != "" {
		argv = append(argv, "--assignee", in.Assignee)
	} else if in.ProjectGID == "" {
		// A task with neither project nor assignee would land in nobody's My
		// Tasks (and be findable only via search); default to the user.
		argv = append(argv, "--assignee", "me")
	}
	return asResult(s.runDharma(ctx, ws, argv...)), nil, nil
}

type commentTaskArgs struct {
	TaskGID string `json:"task_gid" jsonschema:"Task gid"`
	Text    string `json:"text" jsonschema:"Comment text"`
}

func (s *server) commentTask(ctx context.Context, req *mcp.CallToolRequest, in commentTaskArgs) (*mcp.CallToolResult, any, error) {
	return asResult(s.runDharma(ctx, noWorkspace, "task", "comment", "--text", in.Text, "--", in.TaskGID)), nil, nil
}

type completeTaskArgs struct {
	TaskGID string `json:"task_gid" jsonschema:"Task gid"`
}

func (s *server) completeTask(ctx context.Context, req *mcp.CallToolRequest, in completeTaskArgs) (*mcp.CallToolResult, any, error) {
	return asResult(s.runDharma(ctx, noWorkspace, "task", "complete", "--", in.TaskGID)), nil, nil
}

type setDueDateArgs struct {
	TaskGID string `json:"task_gid" jsonschema:"Task gid"`
	Due     string `json:"due,omitempty" jsonschema:"Due date: YYYY-MM-DD, 'today', 'tomorrow', or ISO datetime"`
	Clear   bool   `json:"clear,omitempty" jsonschema:"Clear the due date instead of setting one"`
}

func (s *server) setDueDate(ctx context.Context, req *mcp.CallToolRequest, in setDueDateArgs) (*mcp.CallToolResult, any, error) {
	if in.Due != "" && in.Clear {
		return nil, nil, errors.New("provide either due or clear, not both")
	}
	argv := []string{"task", "set-due"}
	switch {
	case in.Clear:
		argv = append(argv, "--clear")
	case in.Due != "":
		argv = append(argv, "--due", in.Due)
	default:
		return nil, nil, errors.New("provide either due or clear")
	}
	argv = append(argv, "--", in.TaskGID)
	return asResult(s.runDharma(ctx, noWorkspace, argv...)), nil, nil
}

type asanaAPIArgs struct {
	Method string `json:"method,omitempty" jsonschema:"HTTP method: GET, POST, PUT, PATCH, or DELETE (default GET)"`
	Path   string `json:"path" jsonschema:"API path, e.g. /users/me or /tasks/123"`
	// Field's description is set in asanaAPISchema, not here: it begins with
	// "key=value", and jsonschema-go rejects a struct tag starting with WORD=.
	Field    []string `json:"field,omitempty"`
	Body     string   `json:"body,omitempty" jsonschema:"Raw JSON body, passed through unchanged"`
	Paginate bool     `json:"paginate,omitempty" jsonschema:"Follow all pages (GET only)"`
}

func (s *server) asanaAPI(ctx context.Context, req *mcp.CallToolRequest, in asanaAPIArgs) (*mcp.CallToolResult, any, error) {
	method := in.Method
	if method == "" {
		method = "GET"
	}
	argv := []string{"api", "-X", method}
	for _, f := range in.Field {
		argv = append(argv, "-f", f)
	}
	if in.Body != "" {
		argv = append(argv, "--body", in.Body)
	}
	if in.Paginate {
		argv = append(argv, "--paginate")
	}
	argv = append(argv, "--", in.Path)
	return asResult(s.runDharma(ctx, noWorkspace, argv...)), nil, nil
}
