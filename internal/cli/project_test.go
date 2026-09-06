package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const projectGetTestToken = "project-get-test-token"

type projectGetRequest struct {
	Method        string `json:"method"`
	Path          string `json:"path"`
	RawQuery      string `json:"raw_query"`
	Authorization string `json:"authorization"`
	Body          string `json:"body"`
}

type projectGetRoundTripper func(*http.Request) (*http.Response, error)

func (f projectGetRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestProjectGetHelperProcess runs one real Execute invocation in a subprocess.
// Execute calls os.Exit for failures, so process isolation also keeps Cobra's
// package-global command and flag state independent between cases.
func TestProjectGetHelperProcess(t *testing.T) {
	if os.Getenv("DHARMA_PROJECT_GET_HELPER") != "1" {
		return
	}

	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("DHARMA_PROJECT_GET_ARGS")), &args); err != nil {
		t.Fatal(err)
	}
	os.Args = append([]string{"dharma"}, args...)

	recordPath := os.Getenv("DHARMA_PROJECT_GET_RECORD")
	http.DefaultTransport = projectGetRoundTripper(func(req *http.Request) (*http.Response, error) {
		var body []byte
		if req.Body != nil {
			var err error
			body, err = io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
		}
		record, err := json.Marshal(projectGetRequest{
			Method:        req.Method,
			Path:          req.URL.Path,
			RawQuery:      req.URL.RawQuery,
			Authorization: req.Header.Get("Authorization"),
			Body:          string(body),
		})
		if err != nil {
			return nil, err
		}
		f, err := os.OpenFile(recordPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(append(record, '\n')); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}

		status, err := strconv.Atoi(os.Getenv("DHARMA_PROJECT_GET_STATUS"))
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: status,
			Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(os.Getenv("DHARMA_PROJECT_GET_RESPONSE"))),
			Request:    req,
		}, nil
	})

	Execute("test")
	os.Exit(0)
}

type projectGetResult struct {
	stdout   string
	stderr   string
	exit     int
	requests []projectGetRequest
}

func runProjectGet(t *testing.T, args []string, status int, response string) projectGetResult {
	t.Helper()
	recordPath := filepath.Join(t.TempDir(), "requests.jsonl")
	argsJSON, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestProjectGetHelperProcess$")
	cmd.Env = projectGetEnv(os.Environ(), map[string]string{
		"ASANA_TOKEN":                 projectGetTestToken,
		"ASANA_WORKSPACE":             "",
		"DHARMA_PROJECT_GET_HELPER":   "1",
		"DHARMA_PROJECT_GET_ARGS":     string(argsJSON),
		"DHARMA_PROJECT_GET_RECORD":   recordPath,
		"DHARMA_PROJECT_GET_STATUS":   strconv.Itoa(status),
		"DHARMA_PROJECT_GET_RESPONSE": response,
	})
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	exit := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running helper: %v", err)
		}
		exit = exitErr.ExitCode()
	}

	var requests []projectGetRequest
	raw, err := os.ReadFile(recordPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var request projectGetRequest
		if err := json.Unmarshal(line, &request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
	}
	return projectGetResult{stdout: stdout.String(), stderr: stderr.String(), exit: exit, requests: requests}
}

func projectGetEnv(base []string, replacements map[string]string) []string {
	env := make([]string, 0, len(base)+len(replacements))
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if _, replaced := replacements[key]; !replaced {
			env = append(env, item)
		}
	}
	for key, value := range replacements {
		env = append(env, key+"="+value)
	}
	return env
}

func TestProjectGetRequestFields(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantQuery string
	}{
		{
			name:      "default fields",
			args:      []string{"project", "get", "1234567890"},
			wantQuery: "opt_fields=name%2Carchived%2Cpermalink_url",
		},
		{
			name:      "explicit nested fields",
			args:      []string{"project", "get", "1234567890", "--fields", "name,owner.name,team.name"},
			wantQuery: "opt_fields=name%2Cowner.name%2Cteam.name",
		},
		{
			name: "explicitly empty fields",
			args: []string{"project", "get", "1234567890", "--fields", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runProjectGet(t, tt.args, http.StatusOK, `{"data":{"gid":"1234567890"}}`)
			if result.exit != 0 {
				t.Fatalf("exit = %d, want 0; stderr: %s", result.exit, result.stderr)
			}
			if len(result.requests) != 1 {
				t.Fatalf("requests = %d, want exactly 1", len(result.requests))
			}
			request := result.requests[0]
			if request.Method != http.MethodGet {
				t.Errorf("method = %q, want GET", request.Method)
			}
			if request.Path != "/api/1.0/projects/1234567890" {
				t.Errorf("path = %q", request.Path)
			}
			if request.RawQuery != tt.wantQuery {
				t.Errorf("query = %q, want %q", request.RawQuery, tt.wantQuery)
			}
			if request.Authorization != "Bearer "+projectGetTestToken {
				t.Errorf("authorization = %q", request.Authorization)
			}
			if request.Body != "" {
				t.Errorf("body = %q, want empty", request.Body)
			}
		})
	}
}

func TestProjectGetPreservesObjectResponse(t *testing.T) {
	notes := strings.Repeat("A long note with a quote \" and a newline\nCafé ☕. ", 100)
	response, err := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"gid":                   "1234567890",
			"name":                  "Roadmap",
			"notes":                 notes,
			"owner":                 nil,
			"current_status_update": map[string]interface{}{"title": "On track", "resource_subtype": "project_status_update"},
			"custom":                map[string]interface{}{"arbitrary": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := runProjectGet(t, []string{
		"project", "get", "1234567890", "--fields", "name,notes,owner.name,current_status_update.title",
	}, http.StatusOK, string(response))
	if result.exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", result.exit, result.stderr)
	}
	if len(result.requests) != 1 {
		t.Fatalf("requests = %d, want exactly 1", len(result.requests))
	}

	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(result.stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, result.stdout)
	}
	if len(envelope) != 2 || envelope["ok"] != true {
		t.Fatalf("envelope = %#v, want only ok and data", envelope)
	}
	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data = %#v", envelope["data"])
	}
	if data["notes"] != notes {
		t.Error("notes did not round-trip unchanged")
	}
	if owner, present := data["owner"]; !present || owner != nil {
		t.Errorf("owner = %#v (present=%v), want explicit null", owner, present)
	}
	statusUpdate, ok := data["current_status_update"].(map[string]interface{})
	if !ok || statusUpdate["title"] != "On track" {
		t.Errorf("current_status_update = %#v", data["current_status_update"])
	}
	custom, ok := data["custom"].(map[string]interface{})
	if !ok || custom["arbitrary"] != true {
		t.Errorf("custom = %#v", data["custom"])
	}
}

func TestProjectGetAPIErrors(t *testing.T) {
	tests := []struct {
		status   int
		message  string
		wantExit int
	}{
		{status: http.StatusUnauthorized, message: "Not Authorized", wantExit: 2},
		{status: http.StatusForbidden, message: "Forbidden", wantExit: 1},
		{status: http.StatusNotFound, message: "project: Not a recognized ID", wantExit: 1},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			response, err := json.Marshal(map[string]interface{}{
				"errors": []map[string]string{{"message": tt.message, "help": "Check the project and token."}},
			})
			if err != nil {
				t.Fatal(err)
			}
			result := runProjectGet(t, []string{"project", "get", "1234567890"}, tt.status, string(response))
			if result.exit != tt.wantExit {
				t.Errorf("exit = %d, want %d", result.exit, tt.wantExit)
			}
			if len(result.requests) != 1 {
				t.Errorf("requests = %d, want exactly 1", len(result.requests))
			}
			var envelope map[string]interface{}
			if err := json.Unmarshal([]byte(result.stdout), &envelope); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, result.stdout)
			}
			if envelope["ok"] != false {
				t.Errorf("ok = %#v, want false", envelope["ok"])
			}
			if _, present := envelope["data"]; present {
				t.Error("error output unexpectedly contains success data")
			}
			errorData, ok := envelope["error"].(map[string]interface{})
			if !ok {
				t.Fatalf("error = %#v", envelope["error"])
			}
			if errorData["message"] != tt.message {
				t.Errorf("message = %#v, want %q", errorData["message"], tt.message)
			}
			if errorData["http_status"] != float64(tt.status) {
				t.Errorf("http_status = %#v, want %d", errorData["http_status"], tt.status)
			}
			if result.stderr != "Error: "+tt.message+"\n" {
				t.Errorf("stderr = %q", result.stderr)
			}
		})
	}
}

func TestProjectGetUsageErrorsDoNotRequest(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing gid", args: []string{"project", "get"}},
		{name: "extra gid", args: []string{"project", "get", "123", "456"}},
		{name: "unknown flag", args: []string{"project", "get", "123", "--unknown"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runProjectGet(t, tt.args, http.StatusOK, `{"data":{}}`)
			if result.exit != 3 {
				t.Errorf("exit = %d, want 3; stderr: %s", result.exit, result.stderr)
			}
			if len(result.requests) != 0 {
				t.Errorf("requests = %d, want 0", len(result.requests))
			}
			var envelope map[string]interface{}
			if err := json.Unmarshal([]byte(result.stdout), &envelope); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, result.stdout)
			}
			if envelope["ok"] != false {
				t.Errorf("ok = %#v, want false", envelope["ok"])
			}
		})
	}
}

func TestProjectGetHelp(t *testing.T) {
	result := runProjectGet(t, []string{"project", "get", "--help"}, http.StatusOK, `{"data":{}}`)
	if result.exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", result.exit, result.stderr)
	}
	if len(result.requests) != 0 {
		t.Errorf("requests = %d, want 0", len(result.requests))
	}
	// Only the machine-facing contract: the positional signature and that the
	// standard --fields flag is registered with the documented default. Prose
	// and examples are free to change without breaking this test.
	for _, want := range []string{
		"Usage:\n  dharma project get <gid> [flags]",
		`--fields string`,
		`(default "name,archived,permalink_url")`,
	} {
		if !strings.Contains(result.stdout, want) {
			t.Errorf("help does not contain %q\n%s", want, result.stdout)
		}
	}
}
