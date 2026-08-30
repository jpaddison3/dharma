package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A GET/DELETE/HEAD -f value must stay literal — a leading '@' is a query
// filter, not a file read. This is the security-relevant invariant: expansion
// happens only on body methods.
func TestBuildAPIFieldsQueryValuesAreLiteral(t *testing.T) {
	m, q, err := buildAPIFields([]string{"text=@handle", "opt_fields=name"}, false, io.Discard)
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
	m, q, err := buildAPIFields([]string{"text=@" + path}, true, io.Discard)
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
		_, _, err := buildAPIFields([]string{"no-equals-sign"}, hasBody, io.Discard)
		if err == nil {
			t.Fatalf("hasBody=%v: expected an error for a field without '='", hasBody)
		}
		var usageErr *UsageError
		if !errors.As(err, &usageErr) {
			t.Errorf("hasBody=%v: error = %T(%v), want *UsageError", hasBody, err, err)
		}
	}
}

func TestBuildAPIFieldsWarnsForNumericCharacterReferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rich-text.txt")
	if err := os.WriteFile(path, []byte("<body>It&#x27;s literal</body>\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		fields   []string
		hasBody  bool
		wantWarn string
	}{
		{
			name:     "html_notes inline",
			fields:   []string{"html_notes=<body>It&#39;s literal</body>"},
			hasBody:  true,
			wantWarn: "warning: html_notes contains '&#'; numeric character references are stored literally by Asana — use literal UTF-8 instead\n",
		},
		{
			name:     "html_text after file expansion",
			fields:   []string{"html_text=@" + path},
			hasBody:  true,
			wantWarn: "warning: html_text contains '&#'; numeric character references are stored literally by Asana — use literal UTF-8 instead\n",
		},
		{
			name:    "literal UTF-8",
			fields:  []string{"html_notes=<body>It's literal</body>"},
			hasBody: true,
		},
		{
			name:    "unrelated field",
			fields:  []string{"notes=It&#39;s plain text"},
			hasBody: true,
		},
		{
			name:    "query field",
			fields:  []string{"html_notes=<body>It&#39;s literal</body>"},
			hasBody: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var warnings bytes.Buffer
			_, _, err := buildAPIFields(tt.fields, tt.hasBody, &warnings)
			if err != nil {
				t.Fatal(err)
			}
			if got := warnings.String(); got != tt.wantWarn {
				t.Errorf("warning = %q, want %q", got, tt.wantWarn)
			}
		})
	}
}

func TestWarnBodyNumericCharacterReferences(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantWarn string
	}{
		{
			name:     "html_notes in envelope",
			body:     `{"data":{"html_notes":"<body>It&#39;s literal</body>"}}`,
			wantWarn: "warning: html_notes contains '&#'; numeric character references are stored literally by Asana — use literal UTF-8 instead\n",
		},
		{
			name: "literal UTF-8",
			body: `{"data":{"html_notes":"<body>It's literal</body>"}}`,
		},
		{
			name: "numeric reference outside rich text fields",
			body: `{"data":{"notes":"It&#39;s plain text"}}`,
		},
		{
			name: "non-object body",
			body: `[1, 2]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v interface{}
			if err := json.Unmarshal([]byte(tt.body), &v); err != nil {
				t.Fatal(err)
			}
			var warnings bytes.Buffer
			warnBodyNumericCharacterReferences(v, &warnings)
			if got := warnings.String(); got != tt.wantWarn {
				t.Errorf("warning = %q, want %q", got, tt.wantWarn)
			}
		})
	}
}

func TestAPIHelpDocumentsRichText(t *testing.T) {
	for _, want := range []string{
		"Rich text (html_notes / html_text)",
		"Wrap the entire value in <body>...</body>",
		"Numeric references such as &#x27;",
		"dharma api -X PUT /tasks/123 -f html_notes=@-",
		"dharma api -X POST /tasks/123/stories -f html_text=@-",
	} {
		if !strings.Contains(apiCmd.Long, want) {
			t.Errorf("api help does not contain %q", want)
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
