package output

import (
	"encoding/json"
	"os"
	"testing"
)

// capture runs fn against a temp file (never a TTY, so output is compact) and
// returns the decoded top-level JSON object.
func capture(t *testing.T, fn func(w *os.File) error) map[string]interface{} {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	if err := fn(f); err != nil {
		t.Fatal(err)
	}
	f.Close()
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("output was not a JSON object: %v (%s)", err, b)
	}
	return m
}

func TestPrintListPopulated(t *testing.T) {
	m := capture(t, func(w *os.File) error {
		return PrintList(w, []interface{}{"a", "b"}, true, "more pages")
	})
	if m["ok"] != true {
		t.Errorf("ok = %v", m["ok"])
	}
	if m["count"] != float64(2) {
		t.Errorf("count = %v, want 2", m["count"])
	}
	if m["has_more"] != true {
		t.Errorf("has_more = %v, want true", m["has_more"])
	}
	if m["hint"] != "more pages" {
		t.Errorf("hint = %v", m["hint"])
	}
	if data, ok := m["data"].([]interface{}); !ok || len(data) != 2 {
		t.Errorf("data = %#v", m["data"])
	}
}

func TestPrintListEmptyOmitsHint(t *testing.T) {
	m := capture(t, func(w *os.File) error {
		return PrintList(w, []interface{}{}, false, "")
	})
	if m["count"] != float64(0) {
		t.Errorf("count = %v, want 0", m["count"])
	}
	if _, present := m["hint"]; present {
		t.Error("empty hint should be omitted")
	}
	data, ok := m["data"].([]interface{})
	if !ok {
		t.Fatalf("data should be a JSON array, got %T", m["data"])
	}
	if len(data) != 0 {
		t.Errorf("data len = %d, want 0", len(data))
	}
}

func TestPrintListNilSliceIsEmptyArray(t *testing.T) {
	var nilSlice []string
	m := capture(t, func(w *os.File) error {
		return PrintList(w, nilSlice, false, "")
	})
	// A nil slice must serialize as [] (definitive empty), never JSON null.
	if m["data"] == nil {
		t.Fatal("nil slice rendered as null, want []")
	}
	if data, ok := m["data"].([]interface{}); !ok || len(data) != 0 {
		t.Errorf("data = %#v", m["data"])
	}
}

func TestPrintObject(t *testing.T) {
	m := capture(t, func(w *os.File) error {
		return PrintObject(w, map[string]string{"name": "x"})
	})
	if m["ok"] != true {
		t.Errorf("ok = %v", m["ok"])
	}
	data, ok := m["data"].(map[string]interface{})
	if !ok || data["name"] != "x" {
		t.Errorf("data = %#v", m["data"])
	}
}

// PrintObject delegates to PrintObjectFull with nil context/truncated_fields;
// both are omitempty, so a bare object envelope must be exactly {ok,data} — no
// stray context/truncated_fields keys the mcpb shim would then have to handle.
func TestPrintObjectOmitsContextAndTruncated(t *testing.T) {
	m := capture(t, func(w *os.File) error {
		return PrintObject(w, map[string]string{"name": "x"})
	})
	if _, present := m["context"]; present {
		t.Error("context must be omitted when nil")
	}
	if _, present := m["truncated_fields"]; present {
		t.Error("truncated_fields must be omitted when nil")
	}
	if len(m) != 2 {
		t.Errorf("bare object envelope should have only ok+data, got %#v", m)
	}
}

// The `context` block and `truncated_fields` are the PR's headline additions and
// the exact keys agents/the shim read — assert they serialize under those names.
func TestPrintObjectFullContextAndTruncated(t *testing.T) {
	m := capture(t, func(w *os.File) error {
		return PrintObjectFull(w,
			map[string]interface{}{"name": "x", "notes": "shortened…"},
			map[string]interface{}{"comments": float64(3)},
			[]string{"notes"},
		)
	})
	ctx, ok := m["context"].(map[string]interface{})
	if !ok || ctx["comments"] != float64(3) {
		t.Errorf("context = %#v", m["context"])
	}
	tf, ok := m["truncated_fields"].([]interface{})
	if !ok || len(tf) != 1 || tf[0] != "notes" {
		t.Errorf("truncated_fields = %#v", m["truncated_fields"])
	}
}

func TestPrintListFullTruncatedFields(t *testing.T) {
	m := capture(t, func(w *os.File) error {
		return PrintListFull(w,
			[]interface{}{map[string]interface{}{"text": "shortened…"}},
			false, "", []string{"text"},
		)
	})
	tf, ok := m["truncated_fields"].([]interface{})
	if !ok || len(tf) != 1 || tf[0] != "text" {
		t.Errorf("truncated_fields = %#v", m["truncated_fields"])
	}
}
