package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// isInteractive reports whether f is a real terminal. Used to decide whether to
// print a "reading from stdin" hint, so it must be true only for an interactive
// user — term.IsTerminal (already used by auth.go's readSecret) is the right
// test: os.ModeCharDevice also matches /dev/null, which would fire the hint on
// every non-interactive `< /dev/null` caller (e.g. a bare Claude Code Bash run).
func isInteractive(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// stdinReader is where readAllStdin reads from. A package var so tests can
// inject a strings.Reader instead of the real os.Stdin.
var stdinReader io.Reader = os.Stdin

// stdinConsumed tracks whether stdinReader has already been drained, since a
// second read would silently return nothing rather than fail loudly.
var stdinConsumed bool

// readAllStdin reads all of stdinReader. Only one '-'/'@-' input is allowed
// per invocation — stdin can only be consumed once — so a second call errors
// instead of returning an empty string.
func readAllStdin() (string, error) {
	if stdinConsumed {
		return "", usageErrorf("stdin already consumed — only one '-'/'@-' input per invocation")
	}
	stdinConsumed = true
	b, err := io.ReadAll(stdinReader)
	if err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}
	return string(b), nil
}

// expandAtValue expands v when it is an @-reference: "@-" reads stdin, "@path"
// reads a file. Any other value, including a bare "-", passes through
// unchanged. The expanded value has exactly one trailing "\n" stripped,
// matching what $(cat file) would produce.
func expandAtValue(v string) (string, error) {
	if !strings.HasPrefix(v, "@") {
		return v, nil
	}
	rest := v[1:]
	if rest == "-" {
		s, err := readAllStdin()
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(s, "\n"), nil
	}
	b, err := os.ReadFile(rest)
	if err != nil {
		return "", usageErrorf("reading @%s: %v", rest, err)
	}
	return strings.TrimSuffix(string(b), "\n"), nil
}
