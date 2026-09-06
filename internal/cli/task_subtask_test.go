package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jpaddison3/dharma/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type subtaskRequest struct {
	method string
	path   string
	query  url.Values
	data   map[string]interface{}
}

func captureSubtaskRequests(t *testing.T, respond func(*http.Request, int) (int, string)) *[]subtaskRequest {
	t.Helper()
	original := http.DefaultTransport
	requests := []subtaskRequest{}
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured := subtaskRequest{
			method: req.Method,
			path:   req.URL.Path,
			query:  req.URL.Query(),
		}
		if req.Body != nil {
			var envelope struct {
				Data map[string]interface{} `json:"data"`
			}
			if err := json.NewDecoder(req.Body).Decode(&envelope); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			captured.data = envelope.Data
		}
		requests = append(requests, captured)
		status, response := respond(req, len(requests)-1)
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(response)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	return &requests
}

func runSubtaskCommand(t *testing.T, cmd *cobra.Command, argv []string) (string, string, error) {
	t.Helper()
	resetSubtaskCommandState()
	originalToken, originalWorkspace, originalFormat, originalCommandRan := flagToken, flagWorkspace, output.Format, commandRan
	flagToken, flagWorkspace, output.Format, commandRan = "dummy-token", "", "json", false
	t.Cleanup(func() {
		resetSubtaskCommandState()
		flagToken, flagWorkspace, output.Format, commandRan = originalToken, originalWorkspace, originalFormat, originalCommandRan
	})

	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	t.Cleanup(func() { cmd.SetErr(nil) })
	if err := cmd.Flags().Parse(argv); err != nil {
		return "", stderr.String(), err
	}
	args := cmd.Flags().Args()
	if cmd.Args != nil {
		if err := cmd.Args(cmd, args); err != nil {
			return "", stderr.String(), err
		}
	}
	commandRan = true

	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = stdout
	defer func() { os.Stdout = originalStdout }()

	err = cmd.RunE(cmd, args)
	if _, seekErr := stdout.Seek(0, 0); seekErr != nil {
		t.Fatal(seekErr)
	}
	b, readErr := io.ReadAll(stdout)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(b), stderr.String(), err
}

func resetSubtaskCommandState() {
	taskSubtaskListFields = defaultTaskListFields
	taskSubtaskListLimit = 100
	taskSubtaskListPaginate = false
	taskSubtaskCreateName = ""
	taskSubtaskCreateProjects = nil
	taskSubtaskCreateNotes = ""
	taskSubtaskCreateHTMLNotes = ""
	taskSubtaskCreateAssignee = ""
	taskSetParentParent = ""
	taskSetParentClear = false
	for _, cmd := range []*cobra.Command{taskSubtaskListCmd, taskSubtaskCreateCmd, taskSetParentCmd} {
		cmd.Flags().VisitAll(func(flag *pflag.Flag) { flag.Changed = false })
	}
}

func decodeJSONEnvelope(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output %q: %v", raw, err)
	}
	return got
}

func TestTaskSubtaskCommandsRegistered(t *testing.T) {
	subtask, _, err := taskCmd.Find([]string{"subtask"})
	if err != nil || subtask != taskSubtaskCmd {
		t.Fatalf("task subtask registration = (%v, %v), want taskSubtaskCmd", subtask, err)
	}
	for _, name := range []string{"list", "create"} {
		leaf, _, err := taskCmd.Find([]string{"subtask", name})
		if err != nil || leaf == taskSubtaskCmd || leaf.Name() != name {
			t.Errorf("task subtask %s registration = (%v, %v)", name, leaf, err)
		}
	}
	setParent, _, err := taskCmd.Find([]string{"set-parent"})
	if err != nil || setParent != taskSetParentCmd {
		t.Fatalf("task set-parent registration = (%v, %v), want taskSetParentCmd", setParent, err)
	}
}

func TestTaskSubtaskCreatePlacementAndAssignee(t *testing.T) {
	t.Setenv("ASANA_WORKSPACE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	for _, tc := range []struct {
		name         string
		assigneeArgs []string
		wantAssignee string
		wantPresent  bool
	}{
		{name: "omitted"},
		{name: "me", assigneeArgs: []string{"--assignee", "me"}, wantAssignee: "me", wantPresent: true},
		{name: "gid", assigneeArgs: []string{"--assignee", "user-456"}, wantAssignee: "user-456", wantPresent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := captureSubtaskRequests(t, func(*http.Request, int) (int, string) {
				return http.StatusCreated, `{"data":{"gid":"child-returned","name":"Child"}}`
			})
			argv := []string{"--name", "Child", "--project", "project-a", "--project", "project-b"}
			argv = append(argv, tc.assigneeArgs...)
			argv = append(argv, "parent-123")
			stdout, _, err := runSubtaskCommand(t, taskSubtaskCreateCmd, argv)
			if err != nil {
				t.Fatal(err)
			}
			if len(*requests) != 1 {
				t.Fatalf("requests = %d, want one POST and no workspace lookup", len(*requests))
			}
			req := (*requests)[0]
			if req.method != http.MethodPost || req.path != "/api/1.0/tasks/parent-123/subtasks" {
				t.Errorf("request = %s %s", req.method, req.path)
			}
			if _, present := req.data["workspace"]; present {
				t.Errorf("workspace unexpectedly sent: %#v", req.data)
			}
			if got := req.data["name"]; got != "Child" {
				t.Errorf("name = %#v", got)
			}
			if got := req.data["projects"]; !reflect.DeepEqual(got, []interface{}{"project-a", "project-b"}) {
				t.Errorf("projects = %#v", got)
			}
			gotAssignee, present := req.data["assignee"]
			if present != tc.wantPresent || (present && gotAssignee != tc.wantAssignee) {
				t.Errorf("assignee = %#v (present=%v)", gotAssignee, present)
			}
			out := decodeJSONEnvelope(t, stdout)
			if out["ok"] != true || out["data"].(map[string]interface{})["gid"] != "child-returned" {
				t.Errorf("output = %#v", out)
			}
		})
	}
}

func TestTaskSubtaskCreateWithoutPlacementSkipsWorkspaceLookup(t *testing.T) {
	t.Setenv("ASANA_WORKSPACE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	requests := captureSubtaskRequests(t, func(*http.Request, int) (int, string) {
		return http.StatusCreated, `{"data":{"gid":"child"}}`
	})
	if _, _, err := runSubtaskCommand(t, taskSubtaskCreateCmd, []string{"--name", "Child", "parent"}); err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 1 || (*requests)[0].path != "/api/1.0/tasks/parent/subtasks" {
		t.Fatalf("requests = %#v, want only POST /tasks/parent/subtasks", *requests)
	}
	for _, field := range []string{"workspace", "projects", "assignee"} {
		if _, present := (*requests)[0].data[field]; present {
			t.Errorf("%s unexpectedly sent: %#v", field, (*requests)[0].data)
		}
	}
}

func TestTaskSubtaskCreateDescriptions(t *testing.T) {
	rich := `<body>It's "quoted"` + "\n" + `café → next &amp; <a data-asana-gid="123"/></body>`
	path := filepath.Join(t.TempDir(), "description.html")
	if err := os.WriteFile(path, []byte(rich+"\n\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		argv        []string
		stdin       string
		wantField   string
		wantValue   string
		wantPresent bool
	}{
		{name: "plain literal", argv: []string{"--notes", "literal @-\nIt's café"}, wantField: "notes", wantValue: "literal @-\nIt's café", wantPresent: true},
		{name: "rich literal", argv: []string{"--html-notes", rich}, wantField: "html_notes", wantValue: rich, wantPresent: true},
		{name: "rich file trims one LF", argv: []string{"--html-notes", "@" + path}, wantField: "html_notes", wantValue: rich + "\n", wantPresent: true},
		{name: "rich stdin", argv: []string{"--html-notes", "@-"}, stdin: rich + "\n", wantField: "html_notes", wantValue: rich, wantPresent: true},
		{name: "description omitted"},
		{name: "empty plain omitted", argv: []string{"--notes", ""}, wantField: "notes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requests := captureSubtaskRequests(t, func(*http.Request, int) (int, string) {
				return http.StatusOK, `{"data":{"gid":"child"}}`
			})
			if tc.stdin != "" {
				withStdin(t, tc.stdin)
			} else if strings.Contains(strings.Join(tc.argv, " "), "literal @-") {
				withStdin(t, "must not be read")
			}
			argv := append([]string{"--name", "Child"}, tc.argv...)
			argv = append(argv, "parent")
			if _, _, err := runSubtaskCommand(t, taskSubtaskCreateCmd, argv); err != nil {
				t.Fatal(err)
			}
			got, present := (*requests)[0].data[tc.wantField]
			if present != tc.wantPresent || (present && got != tc.wantValue) {
				t.Errorf("%s = %#v (present=%v), want %#v (present=%v)", tc.wantField, got, present, tc.wantValue, tc.wantPresent)
			}
			if tc.name == "plain literal" && stdinConsumed {
				t.Error("plain @- value consumed stdin")
			}
		})
	}
}

func TestTaskSubtaskCreateValidatesBeforeIO(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.html")
	tests := []struct {
		name         string
		argv         []string
		wantCode     int
		checkNoStdin bool
	}{
		{name: "missing name", argv: []string{"parent"}, wantCode: 3},
		{name: "conflicting descriptions", argv: []string{"--name", "Child", "--notes", "", "--html-notes", "@-", "parent"}, wantCode: 3, checkNoStdin: true},
		{name: "unreadable html", argv: []string{"--name", "Child", "--html-notes", "@" + missing, "parent"}, wantCode: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requests := captureSubtaskRequests(t, func(*http.Request, int) (int, string) {
				return http.StatusOK, `{"data":{}}`
			})
			if tc.checkNoStdin {
				withStdin(t, "must not be read")
			}
			_, _, err := runSubtaskCommand(t, taskSubtaskCreateCmd, tc.argv)
			if err == nil {
				t.Fatal("expected error")
			}
			if code, _ := classifyError(err); code != tc.wantCode {
				t.Errorf("classifyError = %d, want %d (%v)", code, tc.wantCode, err)
			}
			if len(*requests) != 0 {
				t.Errorf("validation issued %d HTTP request(s)", len(*requests))
			}
			if tc.checkNoStdin && stdinConsumed {
				t.Error("conflict consumed stdin")
			}
		})
	}
}

func TestTaskSubtaskListFirstPageAndPagination(t *testing.T) {
	for _, tc := range []struct {
		name         string
		paginate     bool
		wantRequests int
		wantCount    float64
		wantHasMore  bool
		wantHint     bool
	}{
		{name: "first page", wantRequests: 1, wantCount: 2, wantHasMore: true, wantHint: true},
		{name: "all pages", paginate: true, wantRequests: 2, wantCount: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := captureSubtaskRequests(t, func(req *http.Request, _ int) (int, string) {
				if req.URL.Query().Get("offset") == "page-2" {
					return http.StatusOK, `{"data":[{"gid":"child-3","completed":true}]}`
				}
				return http.StatusOK, `{"data":[{"gid":"child-1","completed":false},{"gid":"child-2","completed":true}],"next_page":{"offset":"page-2"}}`
			})
			argv := []string{"--limit", "2", "--fields", "name,completed,parent.gid"}
			if tc.paginate {
				argv = append(argv, "--paginate")
			}
			argv = append(argv, "parent-exact")
			stdout, _, err := runSubtaskCommand(t, taskSubtaskListCmd, argv)
			if err != nil {
				t.Fatal(err)
			}
			if len(*requests) != tc.wantRequests {
				t.Fatalf("requests = %d, want %d", len(*requests), tc.wantRequests)
			}
			for i, req := range *requests {
				if req.method != http.MethodGet || req.path != "/api/1.0/tasks/parent-exact/subtasks" {
					t.Errorf("request %d = %s %s", i, req.method, req.path)
				}
				if req.query.Get("limit") != "2" || req.query.Get("opt_fields") != "name,completed,parent.gid" {
					t.Errorf("request %d query = %#v", i, req.query)
				}
				if req.query.Get("completed_since") != "" || req.query.Get("completed") != "" {
					t.Errorf("request %d unexpectedly filtered completed tasks: %#v", i, req.query)
				}
			}
			if tc.paginate && (*requests)[1].query.Get("offset") != "page-2" {
				t.Errorf("later-page query lost offset: %#v", (*requests)[1].query)
			}
			out := decodeJSONEnvelope(t, stdout)
			if out["count"] != tc.wantCount || out["has_more"] != tc.wantHasMore {
				t.Errorf("output = %#v", out)
			}
			_, hasHint := out["hint"]
			if hasHint != tc.wantHint {
				t.Errorf("hint presence = %v, want %v: %#v", hasHint, tc.wantHint, out)
			}
			data := out["data"].([]interface{})
			for i, want := range []string{"child-1", "child-2", "child-3"}[:int(tc.wantCount)] {
				if got := data[i].(map[string]interface{})["gid"]; got != want {
					t.Errorf("data[%d].gid = %#v, want %q", i, got, want)
				}
			}
		})
	}
}

func TestTaskSubtaskListFieldsAndEmptyResults(t *testing.T) {
	for _, tc := range []struct {
		name        string
		fieldsArgs  []string
		wantFields  string
		wantPresent bool
	}{
		{name: "default", wantFields: defaultTaskListFields, wantPresent: true},
		{name: "custom", fieldsArgs: []string{"--fields", "name,parent.gid"}, wantFields: "name,parent.gid", wantPresent: true},
		{name: "raw fields", fieldsArgs: []string{"--fields", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := captureSubtaskRequests(t, func(*http.Request, int) (int, string) {
				return http.StatusOK, `{"data":[]}`
			})
			argv := append([]string{}, tc.fieldsArgs...)
			argv = append(argv, "parent")
			stdout, _, err := runSubtaskCommand(t, taskSubtaskListCmd, argv)
			if err != nil {
				t.Fatal(err)
			}
			values, present := (*requests)[0].query["opt_fields"]
			if present != tc.wantPresent || (present && !reflect.DeepEqual(values, []string{tc.wantFields})) {
				t.Errorf("opt_fields = %#v (present=%v), want %q (present=%v)", values, present, tc.wantFields, tc.wantPresent)
			}
			if got := (*requests)[0].query.Get("limit"); got != "100" {
				t.Errorf("default limit = %q, want 100", got)
			}
			out := decodeJSONEnvelope(t, stdout)
			if out["count"] != float64(0) || out["has_more"] != false {
				t.Errorf("output = %#v", out)
			}
			data, ok := out["data"].([]interface{})
			if !ok || data == nil || len(data) != 0 {
				t.Errorf("data = %#v, want []", out["data"])
			}
		})
	}
}

func TestTaskSubtaskListLimitValidation(t *testing.T) {
	for _, limit := range []string{"0", "101", "-1"} {
		t.Run(limit, func(t *testing.T) {
			requests := captureSubtaskRequests(t, func(*http.Request, int) (int, string) {
				return http.StatusOK, `{"data":[]}`
			})
			_, _, err := runSubtaskCommand(t, taskSubtaskListCmd, []string{"--limit", limit, "parent"})
			if err == nil {
				t.Fatal("expected validation error")
			}
			if code, _ := classifyError(err); code != 3 {
				t.Errorf("classifyError = %d, want usage exit 3 (%v)", code, err)
			}
			if len(*requests) != 0 {
				t.Errorf("validation issued %d HTTP request(s)", len(*requests))
			}
		})
	}
}

func TestTaskSetParentChangeAndClear(t *testing.T) {
	for _, tc := range []struct {
		name       string
		argv       []string
		wantParent interface{}
	}{
		{name: "change", argv: []string{"--parent", "new-parent", "child-task"}, wantParent: "new-parent"},
		{name: "clear", argv: []string{"--clear", "child-task"}, wantParent: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := captureSubtaskRequests(t, func(*http.Request, int) (int, string) {
				return http.StatusOK, `{"data":{"gid":"child-task","parent":null}}`
			})
			stdout, _, err := runSubtaskCommand(t, taskSetParentCmd, tc.argv)
			if err != nil {
				t.Fatal(err)
			}
			if len(*requests) != 1 || (*requests)[0].method != http.MethodPost || (*requests)[0].path != "/api/1.0/tasks/child-task/setParent" {
				t.Fatalf("requests = %#v", *requests)
			}
			got, present := (*requests)[0].data["parent"]
			if !present || got != tc.wantParent {
				t.Errorf("parent = %#v (present=%v), want %#v", got, present, tc.wantParent)
			}
			if len((*requests)[0].data) != 1 {
				t.Errorf("body has unexpected fields: %#v", (*requests)[0].data)
			}
			out := decodeJSONEnvelope(t, stdout)
			if out["ok"] != true || out["data"].(map[string]interface{})["gid"] != "child-task" {
				t.Errorf("output = %#v", out)
			}
		})
	}
}

func TestTaskSubtaskInputValidation(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cobra.Command
		argv []string
	}{
		{name: "list no args", cmd: taskSubtaskListCmd},
		{name: "list too many args", cmd: taskSubtaskListCmd, argv: []string{"one", "two"}},
		{name: "list empty gid", cmd: taskSubtaskListCmd, argv: []string{""}},
		{name: "create no args", cmd: taskSubtaskCreateCmd, argv: []string{"--name", "Child"}},
		{name: "create empty gid", cmd: taskSubtaskCreateCmd, argv: []string{"--name", "Child", ""}},
		{name: "set-parent no args", cmd: taskSetParentCmd, argv: []string{"--clear"}},
		{name: "set-parent too many args", cmd: taskSetParentCmd, argv: []string{"--clear", "one", "two"}},
		{name: "set-parent empty gid", cmd: taskSetParentCmd, argv: []string{"--clear", ""}},
		{name: "set-parent missing option", cmd: taskSetParentCmd, argv: []string{"task"}},
		{name: "set-parent empty parent", cmd: taskSetParentCmd, argv: []string{"--parent", "", "task"}},
		{name: "set-parent both", cmd: taskSetParentCmd, argv: []string{"--parent", "parent", "--clear", "task"}},
		{name: "set-parent both with empty parent", cmd: taskSetParentCmd, argv: []string{"--parent", "", "--clear", "task"}},
		{name: "set-parent false clear is not selection", cmd: taskSetParentCmd, argv: []string{"--clear=false", "task"}},
		{name: "set-parent false clear still conflicts", cmd: taskSetParentCmd, argv: []string{"--parent", "parent", "--clear=false", "task"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requests := captureSubtaskRequests(t, func(*http.Request, int) (int, string) {
				return http.StatusOK, `{"data":{}}`
			})
			_, _, err := runSubtaskCommand(t, tc.cmd, tc.argv)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if code, _ := classifyError(err); code != 3 {
				t.Errorf("classifyError = %d, want usage exit 3 (%v)", code, err)
			}
			if len(*requests) != 0 {
				t.Errorf("validation issued %d HTTP request(s)", len(*requests))
			}
		})
	}
}

func TestTaskSubtaskAPIErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		wantCode int
	}{
		{name: "operational", status: http.StatusInternalServerError, wantCode: 1},
		{name: "auth", status: http.StatusUnauthorized, wantCode: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := captureSubtaskRequests(t, func(*http.Request, int) (int, string) {
				return tc.status, `{"errors":[{"message":"stub failure"}]}`
			})
			_, _, err := runSubtaskCommand(t, taskSubtaskCreateCmd, []string{"--name", "Child", "parent"})
			if err == nil {
				t.Fatal("expected API error")
			}
			if code, _ := classifyError(err); code != tc.wantCode {
				t.Errorf("classifyError = %d, want %d (%v)", code, tc.wantCode, err)
			}
			if len(*requests) != 1 {
				t.Errorf("requests = %d, want 1", len(*requests))
			}
		})
	}
}
