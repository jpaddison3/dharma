package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSelectTaskTextFieldPlainIsLiteral(t *testing.T) {
	for _, value := range []string{"@handle", "@-", "<strong>text</strong>", ""} {
		field, got, present, err := selectTaskTextField(
			"notes", "html_notes", value, "", true, false, true, io.Discard,
		)
		if err != nil {
			t.Fatalf("value %q: %v", value, err)
		}
		if field != "notes" || got != value || !present {
			t.Errorf("value %q: got (%q, %q, %v)", value, field, got, present)
		}
	}
}

func TestSelectTaskTextFieldHTMLExpansionAndWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "description.html")
	want := "<body>It's \"ready\"\ncafé → next &amp; <a data-asana-gid=\"123\"/>\n&#39;</body>\n"
	if err := os.WriteFile(path, []byte(want+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var warnings bytes.Buffer
	field, got, present, err := selectTaskTextField(
		"notes", "html_notes", "", "@"+path, false, true, false, &warnings,
	)
	if err != nil {
		t.Fatal(err)
	}
	if field != "html_notes" || got != want || !present {
		t.Errorf("got (%q, %q, %v), want html_notes with exact expanded contents", field, got, present)
	}
	if warnings.String() == "" {
		t.Error("numeric character reference warning was not emitted")
	}
}

func TestSelectTaskTextFieldHTMLBareDashIsLiteral(t *testing.T) {
	withStdin(t, "must not be consumed")
	field, got, present, err := selectTaskTextField(
		"text", "html_text", "", "-", false, true, true, io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if field != "html_text" || got != "-" || !present {
		t.Errorf("got (%q, %q, %v), want literal dash", field, got, present)
	}
	if stdinConsumed {
		t.Error("bare dash consumed stdin")
	}
}

func TestSelectTaskTextFieldValidatesBeforeExpansion(t *testing.T) {
	withStdin(t, "must not be consumed")
	_, _, _, err := selectTaskTextField(
		"text", "html_text", "", "@-", true, true, true, io.Discard,
	)
	if err == nil {
		t.Fatal("expected conflicting fields to fail")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("error = %T(%v), want *UsageError", err, err)
	}
	if stdinConsumed {
		t.Error("conflict consumed stdin before validation")
	}
}

func TestSelectTaskTextFieldRequiredAndEmptyHTML(t *testing.T) {
	for _, tc := range []struct {
		name         string
		htmlPresent  bool
		required     bool
		wantPresent  bool
		wantUsageErr bool
	}{
		{name: "optional absent"},
		{name: "required absent", required: true, wantUsageErr: true},
		{name: "empty html", htmlPresent: true, wantUsageErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, present, err := selectTaskTextField(
				"notes", "html_notes", "", "", false, tc.htmlPresent, tc.required, io.Discard,
			)
			if present != tc.wantPresent {
				t.Errorf("present = %v, want %v", present, tc.wantPresent)
			}
			var usageErr *UsageError
			if errors.As(err, &usageErr) != tc.wantUsageErr {
				t.Errorf("error = %T(%v), wantUsageErr=%v", err, err, tc.wantUsageErr)
			}
		})
	}
}

func TestBuildTaskCreateBody(t *testing.T) {
	tests := []struct {
		name      string
		notes     string
		htmlNotes string
		assignee  string
		notesSet  bool
		htmlSet   bool
		want      map[string]interface{}
		wantErr   bool
	}{
		{
			name: "plain and assignee", notes: "literal @handle", assignee: "me", notesSet: true,
			want: map[string]interface{}{"name": "Task", "notes": "literal @handle", "assignee": "me"},
		},
		{
			name: "html", htmlNotes: "<body><strong>Hi</strong></body>", htmlSet: true,
			want: map[string]interface{}{"name": "Task", "html_notes": "<body><strong>Hi</strong></body>"},
		},
		{
			name: "empty plain omitted", notesSet: true,
			want: map[string]interface{}{"name": "Task"},
		},
		{
			name: "no description", want: map[string]interface{}{"name": "Task"},
		},
		{
			name: "conflict", notesSet: true, htmlSet: true, wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildTaskCreateBody(
				"Task", tt.notes, tt.htmlNotes, tt.assignee,
				tt.notesSet, tt.htmlSet, io.Discard,
			)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("body = %#v, want %#v", got, tt.want)
			}
		})
	}
}
