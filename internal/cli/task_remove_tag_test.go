package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestTaskRemoveTagRequestAndOutput(t *testing.T) {
	calls := 0
	result := runTagCLI(t, "test-token", func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", req.Method)
		}
		if req.URL.Path != "/api/1.0/tasks/task-1/removeTag" {
			t.Errorf("path = %s", req.URL.Path)
		}
		if req.URL.RawQuery != "" {
			t.Errorf("query = %q, want empty", req.URL.RawQuery)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]interface{}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("request body %q: %v", body, err)
		}
		requireJSONEqual(t, got, map[string]interface{}{"data": map[string]interface{}{"tag": "tag-1"}})
		return jsonResponse(200, `{"data":{}}`), nil
	}, "task", "remove-tag", "task-1", "--tag", "tag-1")
	if result.err != nil {
		t.Fatal(result.err)
	}
	if calls != 1 {
		t.Fatalf("HTTP calls = %d, want 1", calls)
	}
	out := decodeObject(t, result.stdout)
	if out["ok"] != true {
		t.Errorf("ok = %v", out["ok"])
	}
	data, ok := out["data"].(map[string]interface{})
	if !ok || len(data) != 0 {
		t.Errorf("data = %#v, want empty object", out["data"])
	}
	for _, key := range []string{"count", "has_more", "hint", "context"} {
		if _, present := out[key]; present {
			t.Errorf("unexpected %q in object envelope: %#v", key, out)
		}
	}
}

func TestTaskRemoveTagRejectsInvalidInputBeforeHTTP(t *testing.T) {
	tests := [][]string{
		{"task", "remove-tag", "task-1"},
		{"task", "remove-tag", "task-1", "--tag", ""},
		{"task", "remove-tag", "--tag", "tag-1"},
		{"task", "remove-tag", "task-1", "extra", "--tag", "tag-1"},
	}
	for _, args := range tests {
		result := runTagCLI(t, "", nil, args...)
		if result.err == nil {
			t.Errorf("%v: expected usage error", args)
			continue
		}
		if result.code != 3 {
			t.Errorf("%v: exit classification = %d, want 3 (%v)", args, result.code, result.err)
		}
	}
}
