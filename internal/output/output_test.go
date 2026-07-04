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
