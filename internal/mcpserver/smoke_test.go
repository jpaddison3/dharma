package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Smoke test mirroring mcpb/smoke.mjs: spawns the real dharma binary over
// stdio (as ChatGPT desktop / Codex would) and exercises it through the SDK
// client against the live API. Requires a live Asana token, so it's opt-in:
//
//	ASANA_TOKEN=... go test ./internal/mcpserver/ -run Smoke -v
//
// Everything that needs no token lives in TestToolContract below, which runs
// by default — see its comment.
func TestSmoke(t *testing.T) {
	if os.Getenv("ASANA_TOKEN") == "" {
		t.Skip("ASANA_TOKEN not set; skipping live MCP smoke test")
	}

	bin := buildDharmaForTest(t)
	ctx := context.Background()
	session := connectSmoke(t, ctx, bin, nil)

	t.Run("whoami", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("whoami: isError, text = %s", textOf(res))
		}
		env := parseEnvelope(t, textOf(res))
		if env["ok"] != true {
			t.Errorf("whoami: ok = %v, want true", env["ok"])
		}
	})

	t.Run("my_tasks", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "my_tasks",
			Arguments: map[string]any{"fields": "name,due_on"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("my_tasks: isError, text = %s", textOf(res))
		}
		env := parseEnvelope(t, textOf(res))
		if env["ok"] != true {
			t.Errorf("my_tasks: ok = %v, want true", env["ok"])
		}
		if _, present := env["count"]; !present {
			t.Error("my_tasks: count missing from envelope")
		}
	})

	t.Run("get_task bad gid", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_task",
			Arguments: map[string]any{"task_gid": "1"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatal("get_task with bad gid: want isError")
		}
		text := textOf(res)
		if !strings.Contains(text, `"ok":false`) {
			t.Errorf("get_task error text missing structured ok:false envelope: %s", text)
		}
		// The status matters: asserting only that *some* http_status is present
		// let this subtest pass on the 401 a stale token produces — green
		// without ever exercising a bad-gid response. Asana answers gid 1 with
		// 403 ("You do not have access to this task"), not 404.
		if !strings.Contains(text, `"http_status":403`) {
			t.Errorf("get_task with an inaccessible gid should report 403: %s", text)
		}
	})

	// "a missing or rejected token produces an instructive tool error, not
	// a crash" — the missing half is covered token-free in TestToolContract;
	// this is the rejected half, which needs the network but no valid token.
	t.Run("rejected token", func(t *testing.T) {
		badTokenSession := connectSmoke(t, ctx, bin, func(cmd *exec.Cmd) {
			cmd.Env = append(filterEnv("ASANA_TOKEN", "ASANA_WORKSPACE", "XDG_CONFIG_HOME"),
				"ASANA_TOKEN=definitely-not-a-real-token", "XDG_CONFIG_HOME="+t.TempDir())
		})
		res, err := badTokenSession.CallTool(ctx, &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatal("whoami with a rejected token: want isError")
		}
		if text := textOf(res); !strings.Contains(text, `"http_status":401`) {
			t.Errorf("rejected-token error = %q, want the structured 401 envelope", text)
		}
	})

}

// buildDharmaForTest builds the real dharma binary (not a mock) into a temp
// dir, so the smoke test exercises the exact re-exec path production uses.
func buildDharmaForTest(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatalf("go list -m: %v", err)
	}
	repoRoot := strings.TrimSpace(string(out))

	bin := filepath.Join(t.TempDir(), "dharma")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/dharma")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/dharma: %v\n%s", err, out)
	}
	return bin
}

// connectSmoke spawns `<bin> mcp` and connects an SDK client to it over
// stdio. modify, if non-nil, can adjust the child's environment before it
// starts (e.g. to simulate a missing token).
func connectSmoke(t *testing.T, ctx context.Context, bin string, modify func(*exec.Cmd)) *mcp.ClientSession {
	t.Helper()
	cmd := exec.Command(bin, "mcp")
	if modify != nil {
		modify(cmd)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "smoke", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// filterEnv returns the current environment with any entries for the given
// keys removed, so a caller can append its own override without risking a
// duplicate (and OS/exec-dependent precedence) entry.
func filterEnv(drop ...string) []string {
	var out []string
outer:
	for _, kv := range os.Environ() {
		for _, key := range drop {
			if strings.HasPrefix(kv, key+"=") {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

func textOf(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		return ""
	}
	return tc.Text
}

// parseEnvelope parses a tool result's text as dharma's JSON envelope,
// splitting off any "[dharma warning] ..." suffix appended from stderr.
func parseEnvelope(t *testing.T, text string) map[string]interface{} {
	t.Helper()
	body, _, _ := strings.Cut(text, "\n\n[dharma warning] ")
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("parse envelope: %v\ntext: %s", err, text)
	}
	return v
}
