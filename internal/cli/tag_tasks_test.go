package cli

import (
	"net/http"
	"net/url"
	"testing"
)

func TestTagTasksDefaultRequestAndOutput(t *testing.T) {
	calls := 0
	result := runTagCLI(t, "test-token", func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", req.Method)
		}
		if req.URL.Path != "/api/1.0/tags/tag-1/tasks" {
			t.Errorf("path = %s", req.URL.Path)
		}
		requireQueryEqual(t, req.URL.Query(), url.Values{
			"limit": {"100"}, "opt_fields": {defaultTaskListFields},
		})
		return jsonResponse(200, `{"data":[{"gid":"task-1","name":"One"}]}`), nil
	}, "tag", "tasks", "tag-1")
	if result.err != nil {
		t.Fatal(result.err)
	}
	if calls != 1 {
		t.Fatalf("HTTP calls = %d, want 1", calls)
	}
	out := decodeObject(t, result.stdout)
	if out["ok"] != true || out["count"] != float64(1) || out["has_more"] != false {
		t.Errorf("unexpected envelope: %#v", out)
	}
}

func TestTagTasksCustomAndEmptyFields(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantFields string
	}{
		{"custom fields and explicit limit", []string{"--fields", "gid,name,tags.gid", "--limit", "25"}, "gid,name,tags.gid"},
		{"empty fields omitted", []string{"--fields", ""}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"tag", "tasks", "tag-2"}, tt.args...)
			result := runTagCLI(t, "test-token", func(req *http.Request) (*http.Response, error) {
				wantQuery := url.Values{}
				wantLimit := "100"
				if tt.wantFields != "" {
					wantLimit = "25"
					wantQuery.Set("opt_fields", tt.wantFields)
				}
				wantQuery.Set("limit", wantLimit)
				requireQueryEqual(t, req.URL.Query(), wantQuery)
				return jsonResponse(200, `{"data":[]}`), nil
			}, args...)
			if result.err != nil {
				t.Fatal(result.err)
			}
		})
	}
}

func TestTagTasksFirstPageHint(t *testing.T) {
	calls := 0
	result := runTagCLI(t, "test-token", func(req *http.Request) (*http.Response, error) {
		calls++
		return jsonResponse(200, `{"data":[{"gid":"task-1"}],"next_page":{"offset":"next"}}`), nil
	}, "tag", "tasks", "tag-1", "--limit", "1")
	if result.err != nil {
		t.Fatal(result.err)
	}
	if calls != 1 {
		t.Errorf("HTTP calls = %d, want one truncated page", calls)
	}
	out := decodeObject(t, result.stdout)
	if out["has_more"] != true {
		t.Errorf("has_more = %v, want true", out["has_more"])
	}
	if out["hint"] != paginateHintFor(true) {
		t.Errorf("hint = %q, want %q", out["hint"], paginateHintFor(true))
	}
}

func TestTagTasksPaginationRetainsQuery(t *testing.T) {
	calls := 0
	result := runTagCLI(t, "test-token", func(req *http.Request) (*http.Response, error) {
		calls++
		q := req.URL.Query()
		switch calls {
		case 1:
			requireQueryEqual(t, q, url.Values{"limit": {"2"}, "opt_fields": {"name,tags.gid"}})
			return jsonResponse(200, `{"data":[{"gid":"task-1"}],"next_page":{"offset":"page-2"}}`), nil
		case 2:
			requireQueryEqual(t, q, url.Values{"limit": {"2"}, "offset": {"page-2"}, "opt_fields": {"name,tags.gid"}})
			return jsonResponse(200, `{"data":[{"gid":"task-2"}]}`), nil
		default:
			t.Fatalf("unexpected request %d", calls)
			return nil, nil
		}
	}, "tag", "tasks", "tag-1", "--paginate", "--limit", "2", "--fields", "name,tags.gid")
	if result.err != nil {
		t.Fatal(result.err)
	}
	if calls != 2 {
		t.Fatalf("HTTP calls = %d, want 2", calls)
	}
	out := decodeObject(t, result.stdout)
	if out["count"] != float64(2) || out["has_more"] != false {
		t.Errorf("unexpected pagination envelope: %#v", out)
	}
}

func TestTagTasksEmptyListIsArray(t *testing.T) {
	result := runTagCLI(t, "test-token", func(req *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"data":[]}`), nil
	}, "tag", "tasks", "tag-1")
	if result.err != nil {
		t.Fatal(result.err)
	}
	out := decodeObject(t, result.stdout)
	data, ok := out["data"].([]interface{})
	if !ok || data == nil || len(data) != 0 {
		t.Errorf("data = %#v, want []", out["data"])
	}
	if out["count"] != float64(0) {
		t.Errorf("count = %v, want 0", out["count"])
	}
}

func TestTagTasksRejectsUnsupportedInputBeforeHTTP(t *testing.T) {
	tests := [][]string{
		{"tag", "tasks", "tag-1", "--limit=-1"},
		{"tag", "tasks", "tag-1", "--limit", "101"},
		{"tag", "tasks"},
		{"tag", "tasks", "tag-1", "extra"},
		{"tag", "tasks", "tag-1", "--incomplete"},
		{"tag", "tasks", "tag-1", "--completed-since", "now"},
		{"tag", "tasks", "tag-1", "--modified-since", "2026-01-01"},
		{"tag", "tasks", "tag-1", "--assignee", "me"},
		{"tag", "tasks", "tag-1", "--project", "project-1"},
		{"tag", "tasks", "tag-1", "--section", "section-1"},
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
