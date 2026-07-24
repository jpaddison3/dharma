package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A GET/DELETE/HEAD -f value must stay literal — a leading '@' is a query
// filter, not a file read. This is the security-relevant invariant: expansion
// happens only on body methods.
func TestBuildAPIFieldsQueryValuesAreLiteral(t *testing.T) {
	m, q, err := buildAPIFields([]string{"text=@handle", "opt_fields=name"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if m != nil {
		t.Errorf("body map should be nil for a query build, got %#v", m)
	}
	if got := q.Get("text"); got != "@handle" {
		t.Errorf("query text = %q, want literal @handle (no file read)", got)
	}
	if got := q.Get("opt_fields"); got != "name" {
		t.Errorf("query opt_fields = %q", got)
	}
}

// On a body method the same '@file' value is expanded to the file's contents.
func TestBuildAPIFieldsBodyExpandsAtFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.txt")
	if err := os.WriteFile(path, []byte("expanded value\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m, q, err := buildAPIFields([]string{"text=@" + path}, true)
	if err != nil {
		t.Fatal(err)
	}
	if q != nil {
		t.Errorf("query should be nil for a body build, got %#v", q)
	}
	if m["text"] != "expanded value" {
		t.Errorf("body text = %q, want file contents with one trailing newline stripped", m["text"])
	}
}

func TestBuildAPIFieldsRejectsMalformed(t *testing.T) {
	for _, hasBody := range []bool{true, false} {
		_, _, err := buildAPIFields([]string{"no-equals-sign"}, hasBody)
		if err == nil {
			t.Fatalf("hasBody=%v: expected an error for a field without '='", hasBody)
		}
		var usageErr *UsageError
		if !errors.As(err, &usageErr) {
			t.Errorf("hasBody=%v: error = %T(%v), want *UsageError", hasBody, err, err)
		}
	}
}

func TestResolveBodyLiteralPassthrough(t *testing.T) {
	got, err := resolveBody(`{"data":{"completed":true}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"data":{"completed":true}}` {
		t.Errorf("got %q, want unchanged literal JSON", got)
	}
}

func TestResolveBodyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(path, []byte("{\"data\":{}}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveBody("@" + path)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"data":{}}` {
		t.Errorf("got %q", got)
	}
}

// --body accepts a bare "-" as an alias for "@-"; both read stdin.
func TestResolveBodyBareDashAndAtDashReadStdin(t *testing.T) {
	for _, spec := range []string{"-", "@-"} {
		withStdin(t, "{\"data\":{}}\n")
		got, err := resolveBody(spec)
		if err != nil {
			t.Fatalf("resolveBody(%q): %v", spec, err)
		}
		if got != `{"data":{}}` {
			t.Errorf("resolveBody(%q) = %q", spec, got)
		}
	}
}

func TestResolveBodyMissingFileErrors(t *testing.T) {
	_, err := resolveBody("@" + filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Errorf("error = %T(%v), want *UsageError", err, err)
	}
}
