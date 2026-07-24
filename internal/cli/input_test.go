package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withStdin points stdinReader at content for the duration of the test and
// restores the real state afterward, so tests can't leak stdin consumption
// into each other.
func withStdin(t *testing.T, content string) {
	t.Helper()
	origReader, origConsumed := stdinReader, stdinConsumed
	stdinReader = strings.NewReader(content)
	stdinConsumed = false
	t.Cleanup(func() {
		stdinReader = origReader
		stdinConsumed = origConsumed
	})
}

func TestExpandAtValueLiteralPassthrough(t *testing.T) {
	got, err := expandAtValue("plain value")
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain value" {
		t.Errorf("got %q, want unchanged literal", got)
	}
}

func TestExpandAtValueStdin(t *testing.T) {
	withStdin(t, "hello from stdin\n")
	got, err := expandAtValue("@-")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello from stdin" {
		t.Errorf("got %q", got)
	}
}

func TestExpandAtValueFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(path, []byte("file contents\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := expandAtValue("@" + path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "file contents" {
		t.Errorf("got %q", got)
	}
}

func TestExpandAtValueSecondStdinReadErrors(t *testing.T) {
	withStdin(t, "once")
	if _, err := expandAtValue("@-"); err != nil {
		t.Fatal(err)
	}
	_, err := expandAtValue("@-")
	if err == nil {
		t.Fatal("expected an error reading stdin a second time")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Errorf("error = %T(%v), want *UsageError", err, err)
	}
}

func TestExpandAtValueMissingFile(t *testing.T) {
	_, err := expandAtValue("@" + filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Errorf("error = %T(%v), want *UsageError", err, err)
	}
}

func TestExpandAtValueStripsExactlyOneTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multi.txt")
	if err := os.WriteFile(path, []byte("line one\nline two\n\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := expandAtValue("@" + path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "line one\nline two\n" {
		t.Errorf("got %q, want exactly one trailing newline stripped", got)
	}
}
