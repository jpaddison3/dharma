package scripts

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const testBin = "/Users/tester/.local/bin/dharma"

// codex_entry_matches decides whether an existing registration is left alone or
// replaced — and replacing it discards the colleague's own env, timeout, and
// approval settings, including the ASANA_WORKSPACE the multi-workspace tool
// error tells them to set. It reads codex's JSON, which is not this repo's
// format to control, so the shapes it has to survive are pinned here.
func TestCodexEntryMatches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry string
		want  bool
	}{
		{
			name: "what this installer writes",
			entry: entryJSON(`"enabled": true,`, testBin, []string{"mcp"},
				`"env": null,`),
			want: true,
		},
		{
			// Older codex (pre-0.46) has no `enabled` field at all; its entries
			// are enabled, and treating them as a mismatch would replace — and
			// wipe — a working registration on every re-run.
			name:  "older codex without an enabled field",
			entry: entryJSON("", testBin, []string{"mcp"}, ""),
			want:  true,
		},
		{
			// Key order is codex's business; matching a serialized shape made
			// an upgrade break on a reordered field.
			name: "fields in a different order",
			entry: "{\n  \"transport\": {\n    \"cwd\": null,\n    \"args\": [\n      \"mcp\"\n    ],\n" +
				"    \"command\": \"" + testBin + "\"\n  },\n  \"enabled\": true\n}\n",
			want: true,
		},
		{
			name: "settings this installer doesn't set are irrelevant",
			entry: entryJSON(`"enabled": true,`, testBin, []string{"mcp"},
				"\"startup_timeout_sec\": 30,\n  \"env\": {\n    \"ASANA_WORKSPACE\": \"111\"\n  },"),
			want: true,
		},
		{
			name:  "different binary",
			entry: entryJSON(`"enabled": true,`, "/opt/homebrew/bin/dharma", []string{"mcp"}, ""),
		},
		{
			name:  "wrong args",
			entry: entryJSON(`"enabled": true,`, testBin, []string{"serve"}, ""),
		},
		{
			name:  "extra args",
			entry: entryJSON(`"enabled": true,`, testBin, []string{"mcp", "--verbose"}, ""),
		},
		{
			name:  "explicitly disabled",
			entry: entryJSON(`"enabled": false,`, testBin, []string{"mcp"}, ""),
		},
		{
			// Whitespace inside a value is part of the value: an arg of "mcp "
			// is not the arg "mcp", and normalizing it away would call a broken
			// entry healthy.
			name:  "an arg that only looks right after whitespace stripping",
			entry: entryJSON(`"enabled": true,`, testBin, []string{"mcp "}, ""),
		},
		{
			name:  "no entry at all",
			entry: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runCodexEntryMatches(t, tc.entry); got != tc.want {
				t.Errorf("codex_entry_matches = %v, want %v\nentry:\n%s", got, tc.want, tc.entry)
			}
		})
	}
}

// entryJSON renders a `codex mcp get dharma --json` payload in codex 0.145.0's
// pretty-printed shape.
func entryJSON(enabled, command string, args []string, extra string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "      \"" + a + "\""
	}
	var b strings.Builder
	b.WriteString("{\n  \"name\": \"dharma\",\n")
	if enabled != "" {
		fmt.Fprintf(&b, "  %s\n", enabled)
	}
	b.WriteString("  \"transport\": {\n    \"type\": \"stdio\",\n")
	fmt.Fprintf(&b, "    \"command\": \"%s\",\n", command)
	b.WriteString("    \"args\": [\n" + strings.Join(quoted, ",\n") + "\n    ]\n")
	b.WriteString("  },\n")
	if extra != "" {
		b.WriteString("  " + extra + "\n")
	}
	b.WriteString("  \"tool_timeout_sec\": null\n}\n")
	return b.String()
}

func runCodexEntryMatches(t *testing.T, entry string) bool {
	t.Helper()
	installer := absPath(t, "install-chatgpt.sh")
	src, err := os.ReadFile(installer)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "codex_entry_matches() {") {
		t.Fatal("install-chatgpt.sh no longer defines codex_entry_matches — update this test's extraction")
	}
	script := fmt.Sprintf(`
set -uo pipefail
BIN=%q
eval "$(sed -n '/^codex_entry_matches() {/,/^}/p' %q)"
codex_entry_matches "$1"
`, testBin, installer)

	cmd := exec.Command("bash", "-c", script, "bash", entry)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true
	}
	var exitErr *exec.ExitError
	if !asExit(err, &exitErr) || exitErr.ExitCode() > 1 {
		t.Fatalf("codex_entry_matches errored: %v\n%s", err, out)
	}
	return false
}

func asExit(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}
