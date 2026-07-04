package cli

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateText(t *testing.T) {
	if s, was := truncateText("short"); was || s != "short" {
		t.Errorf("short string changed: %q was=%v", s, was)
	}

	exact := strings.Repeat("a", truncateLimit)
	if s, was := truncateText(exact); was || s != exact {
		t.Errorf("at-limit string changed: was=%v", was)
	}

	long := strings.Repeat("a", truncateLimit+500)
	s, was := truncateText(long)
	if !was {
		t.Fatal("over-limit string not truncated")
	}
	if !strings.Contains(s, "truncated, 2500 chars total") {
		t.Errorf("marker/count wrong: %q", s)
	}
	if !strings.HasSuffix(s, "rerun with --full)") {
		t.Errorf("suffix wrong: %q", s)
	}

	// Multibyte: must not split a rune, and count is runes not bytes.
	multi := strings.Repeat("é", truncateLimit+300) // 2 bytes per rune
	ms, mwas := truncateText(multi)
	if !mwas {
		t.Fatal("multibyte over-limit not truncated")
	}
	if !utf8.ValidString(ms) {
		t.Error("truncated multibyte string is not valid UTF-8")
	}
	if !strings.Contains(ms, "truncated, 2300 chars total") {
		t.Errorf("multibyte count wrong: %q", ms)
	}
}

func TestExtractNames(t *testing.T) {
	got := extractNames([]interface{}{
		map[string]interface{}{"name": "a"},
		map[string]interface{}{"gid": "no-name"},
		map[string]interface{}{"name": "b"},
	})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("extractNames = %#v", got)
	}
	// Non-array input yields a non-nil empty slice.
	if n := extractNames("nope"); n == nil || len(n) != 0 {
		t.Errorf("extractNames(non-array) = %#v", n)
	}
}

func TestBuildTaskContextFull(t *testing.T) {
	n := 2
	task := map[string]interface{}{
		"attachments": []interface{}{
			map[string]interface{}{"name": "spec.pdf"},
			map[string]interface{}{"name": "notes.txt"},
		},
		"num_subtasks": float64(3),
		"projects":     []interface{}{map[string]interface{}{"name": "Website"}},
	}
	block := buildTaskContext("123", task, &n)

	if block["comments"] != 2 {
		t.Errorf("comments = %v", block["comments"])
	}
	if block["subtasks"] != 3 {
		t.Errorf("subtasks = %v", block["subtasks"])
	}
	if atts, ok := block["attachments"].([]string); !ok || len(atts) != 2 || atts[0] != "spec.pdf" {
		t.Errorf("attachments = %#v", block["attachments"])
	}
	if projs, ok := block["projects"].([]string); !ok || len(projs) != 1 || projs[0] != "Website" {
		t.Errorf("projects = %#v", block["projects"])
	}
	hint, ok := block["hint"].(string)
	if !ok || !strings.Contains(hint, "task stories 123") {
		t.Errorf("hint = %v", block["hint"])
	}
}

func TestBuildTaskContextNoComments(t *testing.T) {
	zero := 0
	block := buildTaskContext("123", map[string]interface{}{}, &zero)
	if block["comments"] != 0 {
		t.Errorf("comments = %v", block["comments"])
	}
	if _, ok := block["hint"]; ok {
		t.Error("hint should be absent when there are no comments")
	}
}

func TestBuildTaskContextNarrowedFieldsOmitted(t *testing.T) {
	n := 1
	// Task fetched with narrowed --fields: no attachments/num_subtasks/projects.
	block := buildTaskContext("123", map[string]interface{}{}, &n)
	for _, k := range []string{"attachments", "subtasks", "projects"} {
		if _, ok := block[k]; ok {
			t.Errorf("%q should be omitted when not fetched, got %v", k, block[k])
		}
	}
}

func TestBuildTaskContextDegraded(t *testing.T) {
	block := buildTaskContext("123", map[string]interface{}{}, nil)
	v, ok := block["comments"]
	if !ok || v != nil {
		t.Errorf("comments should be present and null when the count failed, got %v (present=%v)", v, ok)
	}
	if _, ok := block["hint"]; ok {
		t.Error("hint should be absent when the count failed")
	}
}
