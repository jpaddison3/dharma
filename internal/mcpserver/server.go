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
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/jpaddison3/dharma/internal/config"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Serve runs the dharma MCP server over stdio until ctx is canceled or the
// client disconnects. A client disconnect is a normal end of session, not an
// error: it returns nil so the caller exits 0 (see internal/cli/mcp.go).
func Serve(ctx context.Context, version string) error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving own executable path: %w", err)
	}
	srv := &server{binPath: bin, discardedWorkspaceEnv: sanitizeEnv()}

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "dharma-asana", Version: version}, &mcp.ServerOptions{
		// Never let incidental SDK logging reach stdout, which stdio reserves
		// for the JSON-RPC stream.
		Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	if err := srv.registerTools(mcpServer); err != nil {
		return fmt.Errorf("registering tools: %w", err)
	}
	if err := mcpServer.Run(ctx, &mcp.StdioTransport{}); err != nil && !isDisconnect(err) {
		return err
	}
	return nil
}

// codeServerClosing is the JSON-RPC error code the SDK's connection layer
// reports when the transport shuts down mid-session (jsonrpc2.ErrServerClosing).
// The host closing stdin at quit surfaces as "server is closing: EOF", which is
// how every normal session ends — not a failure worth an exit code.
const codeServerClosing = -32004

// isDisconnect reports whether err is just the client going away.
func isDisconnect(err error) bool {
	var wire *jsonrpc.Error
	if errors.As(err, &wire) && wire.Code == codeServerClosing {
		return true
	}
	return errors.Is(err, io.EOF) || errors.Is(err, context.Canceled)
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

	// discardedWorkspaceEnv is written once at construction, before any
	// handler runs, so it needs no locking.
	discardedWorkspaceEnv bool

	// mu serializes workspace resolution and guards workspaceGID. It is never
	// held by runDharma: the resolved gid travels to the subprocess as an
	// argument, not as shared state a re-entrant read could deadlock on.
	mu           sync.Mutex
	workspaceGID string
}

// noWorkspace is the workspace argument for commands that don't take one (or
// whose workspace the subprocess resolves for itself).
const noWorkspace = ""

// resolveWorkspace returns the workspace gid to hand workspace-scoped
// commands. If ASANA_WORKSPACE is set or the config file has a usable
// default_workspace, the dharma subprocess resolves it itself on every call
// and this returns noWorkspace. Otherwise it lists workspaces once and caches
// a single result for the life of the process. Concurrent first calls
// serialize on the mutex rather than racing duplicate list calls; a failure
// isn't cached, so a transient error isn't sticky. Port of
// ensureWorkspace/fetchSingleWorkspace, index.js:82-119.
//
// A non-numeric default_workspace is ignored the same way sanitizeEnv ignores
// a non-numeric ASANA_WORKSPACE: passing a pasted workspace *name* through
// would turn every workspace-scoped call into an opaque 400, whereas
// auto-resolution either succeeds or explains itself. This path is reachable
// only in the Go port — the Node shim isolated config away entirely — and it
// is exactly where the multi-workspace error below sends people.
func (s *server) resolveWorkspace(ctx context.Context) (string, error) {
	if os.Getenv("ASANA_WORKSPACE") != "" {
		return noWorkspace, nil
	}
	discarded := s.discardedWorkspaceEnv
	if cfg, err := config.Load(); err == nil && cfg.DefaultWorkspace != "" {
		if numericGID.MatchString(cfg.DefaultWorkspace) {
			return noWorkspace, nil
		}
		discarded = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workspaceGID != "" {
		return s.workspaceGID, nil
	}
	gid, err := s.fetchSingleWorkspace(ctx, discarded)
	if err != nil {
		return "", err
	}
	s.workspaceGID = gid
	return gid, nil
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

// fetchSingleWorkspace lists workspaces and returns the only one. discarded
// reports whether a workspace setting the user configured is being ignored,
// so the multi-workspace error can say why the setting they think they made
// isn't in effect.
func (s *server) fetchSingleWorkspace(ctx context.Context, discarded bool) (string, error) {
	res := s.runDharma(ctx, noWorkspace, "workspace", "list")
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
		if discarded {
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

// maxOutputBytes caps what one subprocess may return, mirroring the Node
// shim's execFile maxBuffer (index.js:76). Without a cap, a model-chosen
// `paginate: true` over a large collection can return hundreds of megabytes
// through a long-lived server — useless to the model and a plausible OOM.
const maxOutputBytes = 32 << 20

// errOutputTooLarge is what a capped call reports instead. Unlike Node, which
// hands back the truncated (and therefore unparseable) stdout alongside its
// maxBuffer error, this drops the partial payload: a bounded, actionable
// message is strictly more useful to the model than a mangled JSON prefix.
var errOutputTooLarge = errors.New("dharma output exceeded 32MB and was discarded — narrow the request (drop paginate, add filters, or request fewer fields)")

// limitedBuffer captures at most limit bytes and then fails the write, which
// makes os/exec close the pipe and the subprocess die on its next write —
// the same "stop the runaway command" behavior Node's maxBuffer has.
type limitedBuffer struct {
	buf      bytes.Buffer
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := maxOutputBytes - b.buf.Len(); len(p) > room {
		b.buf.Write(p[:room])
		b.exceeded = true
		return len(p), errOutputTooLarge
	}
	return b.buf.Write(p)
}

// runDharma re-execs the dharma binary with args, capturing stdout/stderr.
// The subprocess inherits the parent's environment (including any
// ASANA_TOKEN and, unlike mcpb/server/index.js, the parent's normal
// XDG_CONFIG_HOME/config.json — this server intentionally does not isolate
// config, since the brief is normal token resolution). workspace, when not
// noWorkspace, is passed down as ASANA_WORKSPACE so commands that take a
// workspace get the auto-resolved one; commands that don't ignore it.
func (s *server) runDharma(ctx context.Context, workspace string, args ...string) dharmaResult {
	cmd := exec.CommandContext(ctx, s.binPath, args...)
	cmd.Env = os.Environ()
	if workspace != noWorkspace {
		cmd.Env = append(cmd.Env, "ASANA_WORKSPACE="+workspace)
	}
	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := dharmaResult{ok: err == nil, stdout: stdout.buf.String(), stderr: stderr.buf.String()}
	if err != nil {
		res.errMsg = err.Error()
	}
	if stdout.exceeded || stderr.exceeded {
		// Whatever the command's own exit status was, the captured output is
		// a truncated fragment — report the cap, not the fragment.
		res = dharmaResult{ok: false, errMsg: errOutputTooLarge.Error()}
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
