package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
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

// TestCleanDisconnect pins what a host sees when it quits: stdin closes, the
// process exits 0, and stdout carried nothing but JSON-RPC. Without it, an SDK
// change to how a shutdown mid-write is reported would silently bring back
// "server exited with status 1" (and, before that, an error envelope written
// onto the JSON-RPC stream) with every test still green.
func TestCleanDisconnect(t *testing.T) {
	cmd := exec.Command(buildDharmaForTest(t), "mcp")
	cmd.Stdin = strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("`dharma mcp` exited %v on a normal disconnect, want 0\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	// Nothing should reach the host's MCP log on a clean session. The SDK's
	// default logger discards, so setting one (as an earlier revision did)
	// would put six INFO lines and an ERROR for this very disconnect in front
	// of a colleague debugging something else.
	if stderr.String() != "" {
		t.Errorf("clean session wrote to stderr, which the host logs:\n%s", stderr.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if line == "" {
			continue
		}
		var msg struct {
			JSONRPC string `json:"jsonrpc"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil || msg.JSONRPC != "2.0" {
			t.Errorf("non-JSON-RPC line on stdout, which is the transport: %q", line)
		}
	}
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

	// The stub accepts anything, so the argv assertions above are only as good
	// as the flags existing in internal/cli. Replay each one against the real
	// binary with no token and an empty config: a well-formed invocation gets
	// all the way to token resolution and exits 2 ("no token found"), while
	// anything cobra rejects — unknown flag, wrong arity, a flag that changed
	// type — exits 3 first. Asserting the exit code catches every parse-class
	// break, not just the ones that produce a known error string.
	t.Run("every argv parses against the real CLI", func(t *testing.T) {
		bin := buildDharmaForTest(t)
		emptyConfig := t.TempDir()
		for _, tc := range tests {
			cmd := exec.Command(bin, tc.want...)
			cmd.Env = append(filterEnv("ASANA_TOKEN", "ASANA_WORKSPACE", "XDG_CONFIG_HOME"), "XDG_CONFIG_HOME="+emptyConfig)
			out, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
				t.Errorf("%s: `dharma %s` exited %v, want 2 (reached auth, i.e. parsed)\n%s",
					tc.name, strings.Join(tc.want, " "), err, out)
			}
		}
	})

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
		res, err := callMyTasks(t, ctx, s)
		if err != nil {
			t.Fatalf("call failed: %v", err)
		}
		if got := stubEnvWorkspace(t, res); got != "111" {
			t.Errorf("subprocess ASANA_WORKSPACE = %q, want 111", got)
		}
		if s.workspaceGID != "111" {
			t.Errorf("cached gid = %q, want 111", s.workspaceGID)
		}
	})

	// The second call takes the cached branch, which a naive "revalidate the
	// cache under the lock" refactor could deadlock exactly like the first one
	// used to.
	t.Run("second call reuses the cache without re-listing", func(t *testing.T) {
		s := freshInstall(t, `{"ok":true,"count":1,"data":[{"gid":"111","name":"80k"}]}`, 0)
		countFile := filepath.Join(t.TempDir(), "workspace-list-calls")
		t.Setenv("STUB_CALL_LOG", countFile)
		for i := 1; i <= 2; i++ {
			res, err := callMyTasks(t, ctx, s)
			if err != nil {
				t.Fatalf("call %d failed: %v", i, err)
			}
			if got := stubEnvWorkspace(t, res); got != "111" {
				t.Errorf("call %d: subprocess ASANA_WORKSPACE = %q, want 111", i, got)
			}
		}
		if n := strings.Count(readStubLog(t, countFile), "workspace list\n"); n != 1 {
			t.Errorf("`workspace list` ran %d times, want 1 (the gid should be cached)", n)
		}
	})

	t.Run("multiple workspaces produce the instructive error", func(t *testing.T) {
		s := freshInstall(t, `{"ok":true,"count":2,"data":[{"gid":"111","name":"80k"},{"gid":"222","name":"Side"}]}`, 0)
		_, err := callMyTasks(t, ctx, s)
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
		_, err := callMyTasks(t, ctx, s)
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

	t.Run("a failed listing keeps the CLI's structured envelope", func(t *testing.T) {
		s := freshInstall(t, `{"ok":false,"error":{"message":"Not Authorized","http_status":401,"help":"run dharma auth login"}}`, 1)
		_, err := callMyTasks(t, ctx, s)
		if err == nil {
			t.Fatal("want an error")
		}
		for _, want := range []string{"http_status", "401", "Not Authorized"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q dropped %q from dharma's envelope", err, want)
			}
		}
	})

	t.Run("failed listing is reported and not cached", func(t *testing.T) {
		s := freshInstall(t, "boom", 1)
		if _, err := callMyTasks(t, ctx, s); err == nil || !strings.Contains(err.Error(), "could not list workspaces") {
			t.Fatalf("err = %v, want a 'could not list workspaces' error", err)
		}
		if s.workspaceGID != "" {
			t.Errorf("cached gid = %q, want a failure not to be cached", s.workspaceGID)
		}
	})

	t.Run("a token that sees no workspaces is told so", func(t *testing.T) {
		s := freshInstall(t, `{"ok":true,"count":0,"data":[]}`, 0)
		if _, err := callMyTasks(t, ctx, s); err == nil || !strings.Contains(err.Error(), "no Asana workspaces visible") {
			t.Fatalf("err = %v, want the no-workspaces message", err)
		}
	})

	t.Run("unparseable listing is reported", func(t *testing.T) {
		s := freshInstall(t, `not json`, 0)
		if _, err := callMyTasks(t, ctx, s); err == nil || !strings.Contains(err.Error(), "could not parse workspace list") {
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

// TestOutputCap covers the bound that replaces the Node shim's 32MB
// maxBuffer, through the real exec path: a runaway command must fail with a
// short, actionable message rather than hand the model a truncated payload.
func TestOutputCap(t *testing.T) {
	t.Run("buffer stops at the cap", func(t *testing.T) {
		withCap(t, 4096)
		var b limitedBuffer
		var lastErr error
		for i := 0; i < 5; i++ {
			if _, err := b.Write(make([]byte, 1024)); err != nil {
				lastErr = err
			}
		}
		if lastErr == nil {
			t.Fatal("writing past the cap should fail the write, which kills the subprocess")
		}
		if !b.exceeded || b.buf.Len() != maxOutputBytes {
			t.Errorf("captured %d bytes (exceeded=%v), want the cap %d", b.buf.Len(), b.exceeded, maxOutputBytes)
		}
	})

	// A child that blows the cap and then keeps running must not hold the tool
	// call open: os/exec closes the pipe, and WaitDelay kills what's left.
	t.Run("a runaway command is capped and reported", func(t *testing.T) {
		withCap(t, 4096)
		s := &server{binPath: writeFloodStub(t)}
		t.Setenv("ASANA_WORKSPACE", "999")
		done := make(chan dharmaResult, 1)
		go func() { done <- s.runDharma(context.Background(), noWorkspace, "flood") }()
		select {
		case res := <-done:
			if res.ok {
				t.Fatal("a capped call must not report success")
			}
			if !strings.Contains(res.errMsg, "exceeded the") {
				t.Errorf("errMsg = %q, want the cap message", res.errMsg)
			}
			if res.stdout != "" {
				t.Errorf("truncated payload should be discarded, got %d bytes", len(res.stdout))
			}
			if text := textOf(asResult(res)); !strings.Contains(text, "narrow the request") {
				t.Errorf("tool result = %q, want the actionable cap message", text)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("runDharma never returned — a capped child was not stopped")
		}
	})
}

// TestStderrCaveatReachesTheModel pins the acceptance criterion that
// success-path caveats (truncation and pagination warnings, which dharma
// writes to stderr) are surfaced into the tool result — end to end through the
// subprocess, not just through a hand-built struct.
func TestStderrCaveatReachesTheModel(t *testing.T) {
	s := &server{binPath: writeStub(t)}
	t.Setenv("ASANA_WORKSPACE", "999")
	t.Setenv("STUB_STDERR", "results truncated")
	res, _, err := s.whoami(context.Background(), nil, whoamiArgs{})
	if err != nil {
		t.Fatal(err)
	}
	text := textOf(res)
	if !strings.Contains(text, "[dharma warning] results truncated") {
		t.Errorf("tool result = %q, want the stderr caveat appended", text)
	}
	if !strings.Contains(text, "user") {
		t.Errorf("tool result = %q, want stdout too — stderr must not replace it", text)
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
if [ -n "${STUB_CALL_LOG:-}" ]; then printf '%s\n' "$*" >> "$STUB_CALL_LOG"; fi
if [ "$1" = "workspace" ] && [ "$2" = "list" ]; then
  printf '%s' "$STUB_WORKSPACE_JSON"
  exit "${STUB_WORKSPACE_EXIT:-0}"
fi
if [ -n "${STUB_STDERR:-}" ]; then printf '%s\n' "$STUB_STDERR" >&2; fi
printf 'ASANA_WORKSPACE=%s\n' "${ASANA_WORKSPACE:-}"
for arg in "$@"; do printf '%s\n' "$arg"; done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// withCap lowers the output cap for one test, so the exec path can be
// exercised without moving 32MB through a pipe.
func withCap(t *testing.T, n int) {
	t.Helper()
	old := maxOutputBytes
	maxOutputBytes = n
	t.Cleanup(func() { maxOutputBytes = old })
}

// writeFloodStub creates a stand-in that blows past the cap in a single write
// (small enough to fit the pipe buffer, so it never blocks), then closes
// stdout and keeps running. That is the shape SIGPIPE cannot stop: with no
// further writes the child never notices the closed pipe, so only the caller
// killing it ends the tool call. Its sleep is far longer than the test's hang
// guard so the guard can't be beaten by the child exiting on its own.
func writeFloodStub(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dharma-flood")
	script := `#!/bin/sh
printf '%8192d' 0
exec 1>&-
sleep 300
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// readStubLog returns what the stub recorded in STUB_CALL_LOG.
func readStubLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading stub call log: %v", err)
	}
	return string(b)
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

// callMyTasks runs a workspace-scoped handler through the hang guard. Every
// workspace-resolution assertion goes through it, so a re-entrant lock
// reintroduced on *any* path (first call or cached) fails one subtest instead
// of wedging the package until the test binary times out.
func callMyTasks(t *testing.T, ctx context.Context, s *server) (*mcp.CallToolResult, error) {
	t.Helper()
	return mustNotHang(t, func() (*mcp.CallToolResult, error) {
		res, _, err := s.myTasks(ctx, nil, myTasksArgs{})
		return res, err
	})
}

// mustNotHang fails loudly instead of blocking forever, so a re-entrant lock
// in workspace resolution shows up as a test failure rather than a timeout on
// the whole package.
func mustNotHang(t *testing.T, call func() (*mcp.CallToolResult, error)) (*mcp.CallToolResult, error) {
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
		return got.res, got.err
	case <-time.After(30 * time.Second):
		t.Fatal("call never returned — workspace resolution deadlocked")
		return nil, nil
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
