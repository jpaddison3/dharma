// Package scripts holds tests for the shell scripts in this directory. There
// is no Go code here — install-chatgpt.sh's register_via_toml rewrites
// ~/.codex/config.toml, a file the Codex CLI, the Codex IDE extension, and
// ChatGPT desktop all parse, so a bad edit breaks MCP servers that have
// nothing to do with dharma. That makes it the highest-blast-radius code in
// the repo and the one piece most worth a permanent net; running it from a Go
// test means `go test ./...` covers it like everything else.
package scripts

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// exit codes register_via_toml uses (1 is any other failure).
const (
	registered    = 0
	foundExisting = 2
)

func TestRegisterViaToml(t *testing.T) {
	for _, tc := range []struct {
		name     string
		config   string // "" means the file doesn't exist yet
		wantCode int
		// wantKept are lines that must survive; wantGone must not.
		wantKept []string
		wantGone []string
	}{
		{
			name: "replaces an existing block and keeps other servers",
			config: `[mcp_servers.other]
command = "x"

[mcp_servers.dharma]
command = "/old/dharma"
args = ["mcp"]

[tui]
theme = "dark"
`,
			wantCode: registered,
			wantKept: []string{`[mcp_servers.other]`, `[tui]`, `theme = "dark"`},
			wantGone: []string{"/old/dharma"},
		},
		{
			// The section-boundary regex must not treat an indented sibling
			// table as part of the dharma block: that deleted someone else's
			// server outright.
			name: "keeps an indented sibling table",
			config: `[mcp_servers.dharma]
command = "/old/dharma"

  [mcp_servers.important]
  command = "keepme"
`,
			wantCode: registered,
			wantKept: []string{`[mcp_servers.important]`, `command = "keepme"`},
			wantGone: []string{"/old/dharma"},
		},
		{
			name: "keeps an env sub-table for the fresh block",
			config: `[mcp_servers.dharma]
command = "/old/dharma"

[mcp_servers.dharma.env]
ASANA_WORKSPACE = "111"
`,
			wantCode: registered,
			wantKept: []string{`[mcp_servers.dharma.env]`, `ASANA_WORKSPACE = "111"`},
			wantGone: []string{"/old/dharma"},
		},
		{name: "creates a missing config", config: "", wantCode: registered},
		{
			name:     "creates a config from an empty file",
			config:   "\n",
			wantCode: registered,
		},
		// Every spelling below declares mcp_servers.dharma in a form the strip
		// can't remove. Appending a second declaration would make the whole
		// file unparseable for every Codex app, so the only safe answer is to
		// leave the file alone and say so.
		{
			name:     "bails on a quoted header",
			config:   "[mcp_servers.\"dharma\"]\ncommand = \"/old/dharma\"\n",
			wantCode: foundExisting,
		},
		{
			name:     "bails on a single-quoted header",
			config:   "[mcp_servers.'dharma']\ncommand = \"/old/dharma\"\n",
			wantCode: foundExisting,
		},
		{
			name:     "bails on a spaced header",
			config:   "[ mcp_servers.dharma ]\ncommand = \"/old/dharma\"\n",
			wantCode: foundExisting,
		},
		{
			name:     "bails on an inline table",
			config:   "[mcp_servers]\ndharma = { command = \"/old/dharma\", args = [\"mcp\"] }\n",
			wantCode: foundExisting,
		},
		{
			name:     "bails on dotted keys",
			config:   "[mcp_servers]\ndharma.command = \"/old/dharma\"\ndharma.args = [\"mcp\"]\n",
			wantCode: foundExisting,
		},
		// Top-level dotted keys declare the same table with no [mcp_servers]
		// header at all, so the strip never sees a header to match.
		{
			name:     "bails on top-level dotted keys",
			config:   "mcp_servers.dharma.command = \"/old/dharma\"\nmcp_servers.dharma.args = [\"mcp\"]\n",
			wantCode: foundExisting,
		},
		{
			name:     "bails on a top-level inline table",
			config:   "mcp_servers.dharma = { command = \"/old/dharma\", args = [\"mcp\"] }\n",
			wantCode: foundExisting,
		},
		{
			name:     "bails on a quoted top-level dotted key",
			config:   "mcp_servers.\"dharma\".command = \"/old/dharma\"\n",
			wantCode: foundExisting,
		},
		{
			name:     "bails on a quoted table name in the header",
			config:   "[\"mcp_servers\".dharma]\ncommand = \"/old/dharma\"\n",
			wantCode: foundExisting,
		},
		{
			name:     "bails on an array-of-tables header",
			config:   "[[mcp_servers.dharma]]\ncommand = \"/old/dharma\"\n",
			wantCode: foundExisting,
		},
		// An inline table is closed by definition, so appending any
		// [mcp_servers.*] header after one is invalid TOML — even when dharma
		// isn't in the file at all.
		{
			name:     "bails on an inline mcp_servers table containing dharma",
			config:   "mcp_servers = { dharma = { command = \"/old/dharma\", args = [\"mcp\"] } }\n",
			wantCode: foundExisting,
		},
		{
			name:     "bails on an inline mcp_servers table without dharma",
			config:   "mcp_servers = { other = { command = \"/x\" } }\n",
			wantCode: foundExisting,
		},
		{
			name:     "bails on an empty inline mcp_servers table",
			config:   "mcp_servers = {}\n",
			wantCode: foundExisting,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			config := filepath.Join(dir, "config.toml")
			if tc.config != "" {
				if err := os.WriteFile(config, []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			code, out := runRegisterViaToml(t, config)
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d\noutput: %s", code, tc.wantCode, out)
			}

			got := readFile(t, config)
			if tc.wantCode != registered {
				// A bail must leave the file exactly as it was.
				if got != tc.config {
					t.Errorf("config was modified on a bail:\n%s", got)
				}
				return
			}

			if n := strings.Count(got, "\n[mcp_servers.dharma]\n"); n != 1 {
				t.Errorf("want exactly one [mcp_servers.dharma] header, got %d:\n%s", n, got)
			}
			if !strings.Contains(got, `args = ["mcp"]`) {
				t.Errorf("registered block missing args:\n%s", got)
			}
			for _, want := range tc.wantKept {
				if !strings.Contains(got, want) {
					t.Errorf("lost %q from the user's config:\n%s", want, got)
				}
			}
			for _, unwanted := range tc.wantGone {
				if strings.Contains(got, unwanted) {
					t.Errorf("stale %q survived:\n%s", unwanted, got)
				}
			}
			assertMode600(t, config)
		})
	}
}

// TestRegisterViaTomlIsIdempotent pins the property the README promises:
// re-running the installer updates in place rather than accumulating blocks or
// blank lines.
func TestRegisterViaTomlIsIdempotent(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(config, []byte("[mcp_servers.other]\ncommand = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var after string
	for i := 1; i <= 3; i++ {
		if code, out := runRegisterViaToml(t, config); code != registered {
			t.Fatalf("run %d: exit = %d, output: %s", i, code, out)
		}
		got := readFile(t, config)
		if i == 2 {
			after = got
		}
		if i == 3 && got != after {
			t.Errorf("run 3 differs from run 2 — not idempotent:\ngot:\n%s\nwant:\n%s", got, after)
		}
		if n := strings.Count(got, "[mcp_servers.dharma]"); n != 1 {
			t.Fatalf("run %d produced %d dharma blocks:\n%s", i, n, got)
		}
	}
	assertMode600(t, config)
}

// TestRegisterViaTomlDoesNotFalseBail guards the other direction: the bail
// pattern must not fire on unrelated content that merely mentions dharma.
func TestRegisterViaTomlDoesNotFalseBail(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config.toml")
	contents := `# dharma = the old way
[projects."/Users/me/dev/dharma"]
trust_level = "trusted"

[mcp_servers.dharma2]
command = "/usr/local/bin/dharma2"

[mcp_servers.other]
command = "/usr/local/bin/dharma-lookalike"
`
	if err := os.WriteFile(config, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := runRegisterViaToml(t, config); code != registered {
		t.Fatalf("exit = %d, want a normal registration; output: %s", code, out)
	}
	got := readFile(t, config)
	for _, want := range []string{`[projects."/Users/me/dev/dharma"]`, `[mcp_servers.dharma2]`} {
		if !strings.Contains(got, want) {
			t.Errorf("unrelated entry %s lost:\n%s", want, got)
		}
	}
}

// runRegisterViaToml extracts register_via_toml from the installer and runs
// just that function against config — no network, no download, nothing
// touching the real ~/.codex.
func runRegisterViaToml(t *testing.T, config string) (int, string) {
	t.Helper()
	script := fmt.Sprintf(`
set -euo pipefail
BIN=%q
eval "$(sed -n '/^register_via_toml() (/,/^)$/p' %q)"
register_via_toml %q
`, "/Users/tester/.local/bin/dharma", installerPath(t), config)

	cmd := exec.Command("bash", "-c", script)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("running register_via_toml: %v\n%s", err, out)
	}
	return exitErr.ExitCode(), string(out)
}

func installerPath(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("install-chatgpt.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatal(err)
	}
	// The extraction below assumes the function is a subshell body; fail loudly
	// rather than silently testing nothing if that ever changes.
	src := readFile(t, abs)
	if !regexp.MustCompile(`(?m)^register_via_toml\(\) \($`).MatchString(src) {
		t.Fatal("install-chatgpt.sh no longer defines `register_via_toml() (` — update the extraction in this test")
	}
	return abs
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(b)
}

func assertMode600(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("config mode = %o, want 600 — it carries other servers' env secrets", mode)
	}
}
