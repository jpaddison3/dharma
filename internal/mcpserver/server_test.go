package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tests in this file need no Asana token and no network, so they run on every
// `go test ./...` — unlike TestSmoke, which is gated on a live token. They
// cover what the port's acceptance criteria are actually about: the advertised
// tool contract, the argv each tool builds, and the workspace resolution the
// author's own machine can never exercise (his config has a default_workspace;
// a colleague's, written by `dharma auth login`, does not).

// TestToolContract drives the real binary over stdio and pins the tool surface
// against mcpb/server/index.js: names, required arguments, and the two
// asana_api schema facts a Go struct tag can't express.
func TestToolContract(t *testing.T) {
	ctx := context.Background()
	session := connectSmoke(t, ctx, buildDharmaForTest(t), nil)

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("names", func(t *testing.T) {
		got := make([]string, len(res.Tools))
		for i, tool := range res.Tools {
			got[i] = tool.Name
		}
		slices.Sort(got)
		want := []string{
			"asana_api", "comment_task", "complete_task", "create_task", "get_task",
			"list_project_tasks", "list_projects", "my_tasks", "search_tasks",
			"set_due_date", "task_stories", "whoami",
		}
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("tools = %v, want %v", got, want)
		}
	})

	// Required sets are the half of "same input schemas" a name comparison
	// misses: they mirror which zod fields lack .optional() in index.js.
	t.Run("required arguments", func(t *testing.T) {
		want := map[string][]string{
			"asana_api":          {"path"},
			"comment_task":       {"task_gid", "text"},
			"complete_task":      {"task_gid"},
			"create_task":        {"name"},
			"get_task":           {"task_gid"},
			"list_project_tasks": {"project_gid"},
			"list_projects":      nil,
			"my_tasks":           nil,
			"search_tasks":       nil,
			"set_due_date":       {"task_gid"},
			"task_stories":       {"task_gid"},
			"whoami":             nil,
		}
		for _, tool := range res.Tools {
			schema := decodeSchema(t, tool.InputSchema)
			got := slices.Clone(schema.Required)
			slices.Sort(got)
			if !slices.Equal(got, want[tool.Name]) {
				t.Errorf("%s required = %v, want %v", tool.Name, got, want[tool.Name])
			}
		}
	})

	t.Run("asana_api method is constrained", func(t *testing.T) {
		var schema schemaShape
		for _, tool := range res.Tools {
			if tool.Name == "asana_api" {
				schema = decodeSchema(t, tool.InputSchema)
			}
		}
		method := schema.Properties["method"]
		want := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
		if !slices.Equal(method.Enum, want) {
			t.Errorf("method enum = %v, want %v", method.Enum, want)
		}
		if string(method.Default) != `"GET"` {
			t.Errorf("method default = %s, want \"GET\"", method.Default)
		}
		// index.js:357's wording, which can't live in a struct tag.
		if wantDesc := "key=value pairs (query params on GET/DELETE, body fields otherwise)"; schema.Properties["field"].Description != wantDesc {
			t.Errorf("field description = %q, want %q", schema.Properties["field"].Description, wantDesc)
		}
	})

	// A handler-returned error must reach the model as a tool error carrying
	// the message, not as a transport failure — the contract every instructive
	// error in this package depends on.
	t.Run("handler error becomes a tool error", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "set_due_date",
			Arguments: map[string]any{"task_gid": "1", "due": "today", "clear": true},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatal("set_due_date with both due and clear: want isError")
		}
		if text := textOf(res); !strings.Contains(text, "provide either due or clear") {
			t.Errorf("set_due_date error text = %q, want the either/or message", text)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		emptyConfigDir := t.TempDir()
		noTokenSession := connectSmoke(t, ctx, buildDharmaForTest(t), func(cmd *exec.Cmd) {
			cmd.Env = append(filterEnv("ASANA_TOKEN", "ASANA_WORKSPACE", "XDG_CONFIG_HOME"), "XDG_CONFIG_HOME="+emptyConfigDir)
		})
		res, err := noTokenSession.CallTool(ctx, &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatal("whoami with no token: want isError")
		}
		if text := textOf(res); !strings.Contains(text, "dharma auth login") {
			t.Errorf("missing-token error text = %q, want it to mention `dharma auth login`", text)
		}
	})
}

// TestArgvBuilders calls each handler against a stub binary and asserts the
// argv it built, tool by tool. The subprocess boundary is the port's real
// surface: every tool is one argv construction, and none of the mutating ones
// can be exercised against the live API without creating junk in Asana.
func TestArgvBuilders(t *testing.T) {
	s := &server{binPath: writeStub(t)}
	// A configured workspace keeps resolution out of the way here; it is
	// exercised on its own in TestWorkspaceResolution.
	t.Setenv("ASANA_WORKSPACE", "999")
	ctx := context.Background()

	tests := []struct {
		name string
		call func() (*mcp.CallToolResult, any, error)
		want []string
	}{
		{"whoami", func() (*mcp.CallToolResult, any, error) {
			return s.whoami(ctx, nil, whoamiArgs{})
		}, []string{"user", "me"}},

		{"my_tasks defaults", func() (*mcp.CallToolResult, any, error) {
			return s.myTasks(ctx, nil, myTasksArgs{})
		}, []string{"my-tasks", "list", "--incomplete", "--limit", "100"}},

		{"my_tasks paginated and filtered", func() (*mcp.CallToolResult, any, error) {
			return s.myTasks(ctx, nil, myTasksArgs{Paginate: true, IncludeCompleted: true, Section: "Main Work", Fields: "name,due_on"})
		}, []string{"my-tasks", "list", "--paginate", "--section", "Main Work", "--fields", "name,due_on"}},

		{"search_tasks", func() (*mcp.CallToolResult, any, error) {
			completed := false
			return s.searchTasks(ctx, nil, searchTasksArgs{Text: "budget", Assignee: "me", Project: "42", Completed: &completed, Fields: "name"})
		}, []string{"task", "search", "--text", "budget", "--assignee", "me", "--project", "42", "--completed=false", "--fields", "name"}},

		{"get_task", func() (*mcp.CallToolResult, any, error) {
			return s.getTask(ctx, nil, getTaskArgs{TaskGID: "-1", Fields: "name", Full: true})
		}, []string{"task", "get", "--fields", "name", "--full", "--", "-1"}},

		{"task_stories", func() (*mcp.CallToolResult, any, error) {
			return s.taskStories(ctx, nil, taskStoriesArgs{TaskGID: "7", Full: true})
		}, []string{"task", "stories", "--full", "--", "7"}},

		{"list_projects", func() (*mcp.CallToolResult, any, error) {
			return s.listProjects(ctx, nil, listProjectsArgs{Paginate: true})
		}, []string{"project", "list", "--paginate"}},

		{"list_project_tasks", func() (*mcp.CallToolResult, any, error) {
			return s.listProjectTasks(ctx, nil, listProjectTasksArgs{ProjectGID: "42", Fields: "name"})
		}, []string{"task", "list", "--project", "42", "--incomplete", "--fields", "name"}},

		// Neither project nor assignee: the task must be assigned to the user,
		// or it lands in nobody's My Tasks and is findable only by search.
		{"create_task bare", func() (*mcp.CallToolResult, any, error) {
			return s.createTask(ctx, nil, createTaskArgs{Name: "write memo"})
		}, []string{"task", "create", "--name", "write memo", "--assignee", "me"}},

		{"create_task in a project", func() (*mcp.CallToolResult, any, error) {
			return s.createTask(ctx, nil, createTaskArgs{Name: "write memo", Notes: "by friday", ProjectGID: "42"})
		}, []string{"task", "create", "--name", "write memo", "--notes", "by friday", "--project", "42"}},

		{"comment_task", func() (*mcp.CallToolResult, any, error) {
			return s.commentTask(ctx, nil, commentTaskArgs{TaskGID: "7", Text: "-- looks good"})
		}, []string{"task", "comment", "--text", "-- looks good", "--", "7"}},

		{"complete_task", func() (*mcp.CallToolResult, any, error) {
			return s.completeTask(ctx, nil, completeTaskArgs{TaskGID: "7"})
		}, []string{"task", "complete", "--", "7"}},

		{"set_due_date due", func() (*mcp.CallToolResult, any, error) {
			return s.setDueDate(ctx, nil, setDueDateArgs{TaskGID: "7", Due: "today"})
		}, []string{"task", "set-due", "--due", "today", "--", "7"}},

		{"set_due_date clear", func() (*mcp.CallToolResult, any, error) {
			return s.setDueDate(ctx, nil, setDueDateArgs{TaskGID: "7", Clear: true})
		}, []string{"task", "set-due", "--clear", "--", "7"}},

		{"asana_api defaults to GET", func() (*mcp.CallToolResult, any, error) {
			return s.asanaAPI(ctx, nil, asanaAPIArgs{Path: "/users/me"})
		}, []string{"api", "-X", "GET", "--", "/users/me"}},

		{"asana_api with fields and body", func() (*mcp.CallToolResult, any, error) {
			return s.asanaAPI(ctx, nil, asanaAPIArgs{Method: "POST", Path: "/tasks", Field: []string{"a=1", "b=2"}, Body: `{"x":1}`, Paginate: true})
		}, []string{"api", "-X", "POST", "-f", "a=1", "-f", "b=2", "--body", `{"x":1}`, "--paginate", "--", "/tasks"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, _, err := tc.call()
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if got := stubArgv(t, res); !slices.Equal(got, tc.want) {
				t.Errorf("argv =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}

	t.Run("set_due_date rejects neither due nor clear", func(t *testing.T) {
		if _, _, err := s.setDueDate(ctx, nil, setDueDateArgs{TaskGID: "7"}); err == nil {
			t.Error("want an error when neither due nor clear is given")
		}
	})
}

// TestWorkspaceResolution covers the path a colleague hits on a fresh install
// — no ASANA_WORKSPACE, no default_workspace — which the author's own machine
// short-circuits past. Its first case is the regression guard for a
// self-deadlock that wedged the whole server here.
func TestWorkspaceResolution(t *testing.T) {
	ctx := context.Background()

	t.Run("single workspace resolves and is passed to the subprocess", func(t *testing.T) {
		s := freshInstall(t, `{"ok":true,"count":1,"data":[{"gid":"111","name":"80k"}]}`, 0)
		res := mustNotHang(t, func() (*mcp.CallToolResult, error) {
			r, _, err := s.myTasks(ctx, nil, myTasksArgs{})
			return r, err
		})
		if got := stubEnvWorkspace(t, res); got != "111" {
			t.Errorf("subprocess ASANA_WORKSPACE = %q, want 111", got)
		}
		if s.workspaceGID != "111" {
			t.Errorf("cached gid = %q, want 111", s.workspaceGID)
		}
	})

	t.Run("multiple workspaces produce the instructive error", func(t *testing.T) {
		s := freshInstall(t, `{"ok":true,"count":2,"data":[{"gid":"111","name":"80k"},{"gid":"222","name":"Side"}]}`, 0)
		_, _, err := s.myTasks(ctx, nil, myTasksArgs{})
		if err == nil {
			t.Fatal("want an error naming both workspaces")
		}
		for _, want := range []string{"80k (111)", "Side (222)", "default_workspace"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q missing %q", err, want)
			}
		}
	})

	t.Run("non-numeric default_workspace is ignored and explained", func(t *testing.T) {
		s := freshInstall(t, `{"ok":true,"count":2,"data":[{"gid":"111","name":"80k"},{"gid":"222","name":"Side"}]}`, 0)
		writeConfig(t, `{"token":"x","default_workspace":"80,000 Hours"}`)
		_, _, err := s.myTasks(ctx, nil, myTasksArgs{})
		if err == nil {
			t.Fatal("want the multi-workspace error, not a call with a workspace name")
		}
		if !strings.Contains(err.Error(), "isn't a numeric gid") {
			t.Errorf("error %q should explain that the configured value was ignored", err)
		}
	})

	t.Run("numeric default_workspace short-circuits resolution", func(t *testing.T) {
		s := freshInstall(t, `{"ok":true,"count":2,"data":[{"gid":"111","name":"80k"},{"gid":"222","name":"Side"}]}`, 0)
		writeConfig(t, `{"token":"x","default_workspace":"333"}`)
		ws, err := s.resolveWorkspace(ctx)
		if err != nil || ws != noWorkspace {
			t.Fatalf("resolveWorkspace = (%q, %v), want (noWorkspace, nil) — the subprocess resolves it itself", ws, err)
		}
	})

	t.Run("failed listing is reported and not cached", func(t *testing.T) {
		s := freshInstall(t, "boom", 1)
		if _, _, err := s.myTasks(ctx, nil, myTasksArgs{}); err == nil || !strings.Contains(err.Error(), "could not list workspaces") {
			t.Fatalf("err = %v, want a 'could not list workspaces' error", err)
		}
		if s.workspaceGID != "" {
			t.Errorf("cached gid = %q, want a failure not to be cached", s.workspaceGID)
		}
	})

	t.Run("unparseable listing is reported", func(t *testing.T) {
		s := freshInstall(t, `not json`, 0)
		if _, _, err := s.myTasks(ctx, nil, myTasksArgs{}); err == nil || !strings.Contains(err.Error(), "could not parse workspace list") {
			t.Fatalf("err = %v, want a parse error", err)
		}
	})
}

func TestParseWorkspaces(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stdout  string
		want    []asanaWorkspace
		wantErr bool
	}{
		{"envelope", `{"ok":true,"count":1,"data":[{"gid":"1","name":"a"}]}`, []asanaWorkspace{{GID: "1", Name: "a"}}, false},
		{"bare array", `[{"gid":"1","name":"a"}]`, []asanaWorkspace{{GID: "1", Name: "a"}}, false},
		{"empty envelope", `{"ok":true,"count":0,"data":[]}`, []asanaWorkspace{}, false},
		{"garbage", `<html>`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseWorkspaces(tc.stdout)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAsResult pins every branch of the result conversion, including the
// success-path caveat the acceptance criteria call for ("truncation/pagination
// warnings surfaced into tool results").
func TestAsResult(t *testing.T) {
	for _, tc := range []struct {
		name        string
		res         dharmaResult
		wantText    string
		wantIsError bool
	}{
		{"failure prefers the structured envelope", dharmaResult{stdout: `{"ok":false}`, stderr: "Error: nope", errMsg: "exit 1"}, `{"ok":false}`, true},
		{"failure falls back to stderr", dharmaResult{stderr: " Error: nope\n", errMsg: "exit 1"}, "Error: nope", true},
		{"failure falls back to the exec error", dharmaResult{errMsg: "fork/exec: no such file"}, "fork/exec: no such file", true},
		{"failure with nothing at all", dharmaResult{}, "dharma failed with no output", true},
		{"success", dharmaResult{ok: true, stdout: `{"ok":true}` + "\n"}, `{"ok":true}`, false},
		{"success with no output", dharmaResult{ok: true}, "(empty response)", false},
		{"success appends a stderr caveat", dharmaResult{ok: true, stdout: `{"ok":true}`, stderr: "results truncated\n"}, "{\"ok\":true}\n\n[dharma warning] results truncated", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := asResult(tc.res)
			if got.IsError != tc.wantIsError {
				t.Errorf("IsError = %v, want %v", got.IsError, tc.wantIsError)
			}
			if text := textOf(got); text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
		})
	}
}

// TestOutputCap covers the bound that replaces the Node shim's 32MB maxBuffer:
// a runaway command must fail with a short, actionable message rather than
// hand a truncated (unparseable) payload to the model.
func TestOutputCap(t *testing.T) {
	var b limitedBuffer
	chunk := make([]byte, 1<<20)
	var lastErr error
	for i := 0; i < (maxOutputBytes>>20)+1; i++ {
		if _, err := b.Write(chunk); err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		t.Fatal("writing past the cap should fail the write, which kills the subprocess")
	}
	if !b.exceeded {
		t.Error("exceeded not set")
	}
	if b.buf.Len() != maxOutputBytes {
		t.Errorf("captured %d bytes, want the cap %d", b.buf.Len(), maxOutputBytes)
	}

	res := asResult(dharmaResult{ok: false, errMsg: errOutputTooLarge.Error()})
	if !res.IsError || !strings.Contains(textOf(res), "exceeded 32MB") {
		t.Errorf("capped result = %q (isError %v), want the cap message", textOf(res), res.IsError)
	}
}

// --- helpers ---------------------------------------------------------------

// freshInstall builds a server pointed at a stub binary, with the environment
// of a colleague who just ran the installer: a token-only config and no
// ASANA_WORKSPACE. workspaceJSON is what the stub prints for `workspace list`.
func freshInstall(t *testing.T, workspaceJSON string, exitCode int) *server {
	t.Helper()
	s := &server{binPath: writeStub(t)}
	t.Setenv("ASANA_WORKSPACE", "")
	os.Unsetenv("ASANA_WORKSPACE")
	t.Setenv("STUB_WORKSPACE_JSON", workspaceJSON)
	t.Setenv("STUB_WORKSPACE_EXIT", fmt.Sprint(exitCode))
	writeConfig(t, `{"token":"x"}`)
	return s
}

// writeConfig points XDG_CONFIG_HOME at a temp dir holding the given
// dharma config, so config.Load() is hermetic.
func writeConfig(t *testing.T, contents string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "dharma"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dharma", "config.json"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
}

// writeStub creates a stand-in for the dharma binary that reports its own
// argv (one argument per line, after an ASANA_WORKSPACE= line) instead of
// calling Asana — so argv assertions need no token, no network, and create
// nothing. `workspace list` is answered from the environment.
func writeStub(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dharma-stub")
	script := `#!/bin/sh
if [ "$1" = "workspace" ] && [ "$2" = "list" ]; then
  printf '%s' "$STUB_WORKSPACE_JSON"
  exit "${STUB_WORKSPACE_EXIT:-0}"
fi
printf 'ASANA_WORKSPACE=%s\n' "${ASANA_WORKSPACE:-}"
for arg in "$@"; do printf '%s\n' "$arg"; done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// stubArgv reads back the argv the stub echoed.
func stubArgv(t *testing.T, res *mcp.CallToolResult) []string {
	t.Helper()
	if res.IsError {
		t.Fatalf("stub call failed: %s", textOf(res))
	}
	lines := strings.Split(textOf(res), "\n")
	return lines[1:] // line 0 is the ASANA_WORKSPACE echo
}

func stubEnvWorkspace(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res.IsError {
		t.Fatalf("stub call failed: %s", textOf(res))
	}
	first, _, _ := strings.Cut(textOf(res), "\n")
	return strings.TrimPrefix(first, "ASANA_WORKSPACE=")
}

// mustNotHang fails loudly instead of blocking forever, so a re-entrant lock
// in workspace resolution shows up as a test failure rather than a timeout on
// the whole package.
func mustNotHang(t *testing.T, call func() (*mcp.CallToolResult, error)) *mcp.CallToolResult {
	t.Helper()
	type outcome struct {
		res *mcp.CallToolResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := call()
		done <- outcome{res, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("call failed: %v", got.err)
		}
		return got.res
	case <-time.After(30 * time.Second):
		t.Fatal("call never returned — workspace resolution deadlocked")
		return nil
	}
}

type schemaShape struct {
	Required   []string `json:"required"`
	Properties map[string]struct {
		Description string          `json:"description"`
		Enum        []string        `json:"enum"`
		Default     json.RawMessage `json:"default"`
	} `json:"properties"`
}

// decodeSchema re-decodes a tool's advertised input schema — the client sees
// it as generic JSON, which is exactly what a host would validate against.
func decodeSchema(t *testing.T, in any) schemaShape {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out schemaShape
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
