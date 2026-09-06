package cli

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/spf13/cobra"
)

func TestTagGetRequestFieldsAndObjectOutput(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantFields string
	}{
		{"default", nil, "name,color"},
		{"custom", []string{"--fields", "gid,name"}, "gid,name"},
		{"raw fields", []string{"--fields", ""}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"tag", "get", "tag-1"}, tt.args...)
			result := runTagCLI(t, "test-token", func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.Path != "/api/1.0/tags/tag-1" {
					t.Errorf("request = %s %s", req.Method, req.URL.Path)
				}
				wantQuery := url.Values{}
				if tt.wantFields != "" {
					wantQuery.Set("opt_fields", tt.wantFields)
				}
				requireQueryEqual(t, req.URL.Query(), wantQuery)
				return jsonResponse(200, `{"data":{"gid":"tag-1","name":"Priority","color":"dark-red"}}`), nil
			}, args...)
			if result.err != nil {
				t.Fatal(result.err)
			}
			out := decodeObject(t, result.stdout)
			if out["ok"] != true {
				t.Errorf("unexpected envelope: %#v", out)
			}
			if _, present := out["count"]; present {
				t.Errorf("tag get returned a list envelope: %#v", out)
			}
			data, ok := out["data"].(map[string]interface{})
			if !ok || data["gid"] != "tag-1" || data["name"] != "Priority" || data["color"] != "dark-red" {
				t.Errorf("unexpected tag data: %#v", out["data"])
			}
		})
	}
}

func TestTagGetWrongArityIsUsageError(t *testing.T) {
	for _, args := range [][]string{{"tag", "get"}, {"tag", "get", "one", "two"}} {
		result := runTagCLI(t, "", nil, args...)
		if result.err == nil || result.code != 3 {
			t.Errorf("%v: err=%v code=%d, want usage error/3", args, result.err, result.code)
		}
	}
}

func TestTagGetAPIErrorClassification(t *testing.T) {
	tests := []struct {
		status   int
		wantCode int
	}{
		{http.StatusUnauthorized, 2},
		{http.StatusNotFound, 1},
	}
	for _, tt := range tests {
		result := runTagCLI(t, "test-token", func(req *http.Request) (*http.Response, error) {
			return jsonResponse(tt.status, `{"errors":[{"message":"tag unavailable"}]}`), nil
		}, "tag", "get", "missing")
		if result.err == nil {
			t.Fatalf("HTTP %d: expected error", tt.status)
		}
		if result.code != tt.wantCode || result.payload.HTTPStatus != tt.status {
			t.Errorf("HTTP %d: code/status = %d/%d, want %d/%d", tt.status, result.code, result.payload.HTTPStatus, tt.wantCode, tt.status)
		}
	}
}

func TestTagAndTaskCommandRegistration(t *testing.T) {
	requireCommands(t, tagCmd, map[string]*cobra.Command{
		"list": tagListCmd, "create": tagCreateCmd, "get": tagGetCmd, "tasks": tagTasksCmd,
	})
	requireCommands(t, taskCmd, map[string]*cobra.Command{
		"add-tag": taskAddTagCmd, "remove-tag": taskRemoveTagCmd,
	})
}
