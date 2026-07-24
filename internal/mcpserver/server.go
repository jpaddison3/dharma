// Package mcpserver exposes the dharma CLI as an MCP server over stdio, for
// hosts that can't shell out to the CLI directly (ChatGPT desktop, Codex).
// It re-execs the running binary per tool call, exactly as mcpb/server/index.js
// shells out to the bundled CLI, so command bodies that print straight to
// os.Stdout never have to change: MCP stdio owns this process's stdout, and
// only the subprocess's captured stdout/stderr become tool output.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/jpaddison3/dharma/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Serve runs the dharma MCP server over stdio until ctx is canceled or the
// client disconnects.
func Serve(ctx context.Context, version string) error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving own executable path: %w", err)
	}
	srv := &server{binPath: bin, discardedWorkspaceSetting: sanitizeEnv()}

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "dharma-asana", Version: version}, &mcp.ServerOptions{
		// Never let incidental SDK logging reach stdout, which stdio reserves
		// for the JSON-RPC stream.
		Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	srv.registerTools(mcpServer)
	return mcpServer.Run(ctx, &mcp.StdioTransport{})
}

// numericGID matches a bare Asana gid (digits only) — the only shape
// ASANA_WORKSPACE is ever valid in.
var numericGID = regexp.MustCompile(`^\d+$`)

// sanitizeEnv trims ASANA_TOKEN/ASANA_WORKSPACE and drops a non-numeric
// ASANA_WORKSPACE before any command runs, so a stray configured value
// (e.g. a pasted workspace name) can't surface as opaque 400s on every call —
// auto-detection, or its instructive multi-workspace error, takes over
// instead. Reports whether a workspace setting was discarded, so the
// multi-workspace error can explain why the setting the user thinks they
// configured isn't in effect. Port of mcpb/server/index.js:20-33, minus the
// ${user_config...} placeholder check, which is Claude-Desktop-specific
// substitution plumbing that doesn't apply here.
func sanitizeEnv() bool {
	for _, key := range []string{"ASANA_TOKEN", "ASANA_WORKSPACE"} {
		v, ok := os.LookupEnv(key)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			os.Unsetenv(key)
		} else if trimmed != v {
			os.Setenv(key, trimmed)
		}
	}
	if ws := os.Getenv("ASANA_WORKSPACE"); ws != "" && !numericGID.MatchString(ws) {
		os.Unsetenv("ASANA_WORKSPACE")
		fmt.Fprintln(os.Stderr, "dharma-asana: ignoring configured Workspace GID — not a numeric gid")
		return true
	}
	return false
}

// server holds the plumbing shared by every tool handler: the path to
// re-exec, and the lazily-resolved workspace gid cache.
type server struct {
	binPath string

	mu                        sync.Mutex
	workspaceGID              string
	discardedWorkspaceSetting bool
}

// ensureWorkspace resolves the workspace gid needed by workspace-scoped
// tools. If ASANA_WORKSPACE is set or the config file has a default_workspace,
// the dharma subprocess resolves it itself on every call — nothing to do
// here. Otherwise this lists workspaces once and caches a single result,
// passed to every subsequent runDharma call explicitly. Concurrent first
// calls serialize on the mutex rather than racing duplicate list calls; a
// failure isn't cached, so a transient error isn't sticky for the life of the
// process. Port of ensureWorkspace/fetchSingleWorkspace, index.js:82-119.
func (s *server) ensureWorkspace(ctx context.Context) error {
	if os.Getenv("ASANA_WORKSPACE") != "" {
		return nil
	}
	if cfg, err := config.Load(); err == nil && cfg.DefaultWorkspace != "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workspaceGID != "" {
		return nil
	}
	gid, err := s.fetchSingleWorkspace(ctx)
	if err != nil {
		return err
	}
	s.workspaceGID = gid
	return nil
}

// cachedWorkspace returns the resolved workspace gid, or "" if none is
// cached yet. Reading under the mutex keeps this race-free against the write
// in ensureWorkspace.
func (s *server) cachedWorkspace() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workspaceGID
}

type asanaWorkspace struct {
	GID  string `json:"gid"`
	Name string `json:"name"`
}

// parseWorkspaces decodes a `workspace list` response. dharma wraps lists as
// {"ok":true,"count":N,"data":[...]}; tolerate a bare array too, so an
// older/newer CLI can't silently break workspace resolution.
func parseWorkspaces(stdout string) ([]asanaWorkspace, error) {
	var arr []asanaWorkspace
	if err := json.Unmarshal([]byte(stdout), &arr); err == nil {
		return arr, nil
	}
	var envelope struct {
		Data []asanaWorkspace `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		return nil, err
	}
	return envelope.Data, nil
}

func (s *server) fetchSingleWorkspace(ctx context.Context) (string, error) {
	res := s.runDharma(ctx, "workspace", "list")
	if !res.ok {
		msg := strings.TrimSpace(res.stderr)
		if msg == "" {
			msg = res.errMsg
		}
		return "", fmt.Errorf("could not list workspaces: %s", msg)
	}
	workspaces, err := parseWorkspaces(res.stdout)
	if err != nil {
		return "", fmt.Errorf("could not parse workspace list: %w", err)
	}
	if len(workspaces) == 0 {
		return "", errors.New("no Asana workspaces visible to this token")
	}
	if len(workspaces) > 1 {
		names := make([]string, len(workspaces))
		for i, w := range workspaces {
			names[i] = fmt.Sprintf("%s (%s)", w.Name, w.GID)
		}
		msg := fmt.Sprintf(
			`your Asana token can see multiple workspaces: %s. Set "default_workspace" in ~/.config/dharma/config.json (or ASANA_WORKSPACE in the MCP server's env config) to the gid of the one to use.`,
			strings.Join(names, ", "),
		)
		if s.discardedWorkspaceSetting {
			msg += " Note: the currently configured Workspace GID was ignored because it isn't a numeric gid."
		}
		return "", errors.New(msg)
	}
	return workspaces[0].GID, nil
}

// dharmaResult is the outcome of one re-exec'd dharma invocation.
type dharmaResult struct {
	ok     bool
	stdout string
	stderr string
	errMsg string
}

// runDharma re-execs the dharma binary with args, capturing stdout/stderr.
// The subprocess inherits the parent's environment (including any
// ASANA_TOKEN and, unlike mcpb/server/index.js, the parent's normal
// XDG_CONFIG_HOME/config.json — this server intentionally does not isolate
// config, since the brief is normal token resolution) plus the cached
// workspace gid, once resolved, so commands that take a workspace get it for
// free; commands that don't just ignore the env var.
func (s *server) runDharma(ctx context.Context, args ...string) dharmaResult {
	cmd := exec.CommandContext(ctx, s.binPath, args...)
	cmd.Env = os.Environ()
	if ws := s.cachedWorkspace(); ws != "" {
		cmd.Env = append(cmd.Env, "ASANA_WORKSPACE="+ws)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := dharmaResult{ok: err == nil, stdout: stdout.String(), stderr: stderr.String()}
	if err != nil {
		res.errMsg = err.Error()
	}
	return res
}

// asResult converts a dharma invocation into a tool result. On failure it
// prefers the structured {"ok":false,"error":{...}} envelope dharma writes to
// stdout (so the model gets http_status/help), falling back to stderr or the
// exec error. On success, a stderr caveat (e.g. "results truncated") is
// appended so the model sees it even though the call succeeded. Port of
// asResult, index.js:121-142.
func asResult(res dharmaResult) *mcp.CallToolResult {
	if !res.ok {
		text := strings.TrimSpace(res.stdout)
		if text == "" {
			text = strings.TrimSpace(res.stderr)
		}
		if text == "" {
			text = res.errMsg
		}
		if text == "" {
			text = "dharma failed with no output"
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
			IsError: true,
		}
	}
	text := strings.TrimSpace(res.stdout)
	if text == "" {
		text = "(empty response)"
	}
	if warning := strings.TrimSpace(res.stderr); warning != "" {
		text += "\n\n[dharma warning] " + warning
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}
