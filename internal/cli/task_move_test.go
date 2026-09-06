package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

const taskMoveHelperEnv = "DHARMA_TASK_MOVE_HELPER"

type taskMoveRequest struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
}

type taskMoveResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Requests []taskMoveRequest
}

type taskMoveRoundTripper func(*http.Request) (*http.Response, error)

func (f taskMoveRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestTaskMoveHelperProcess runs Execute in a subprocess so Cobra's package
// globals and Execute's os.Exit behavior are exercised without leaking between
// cases. A test-only default transport records requests and returns the canned
// Asana response supplied by the parent process.
func TestTaskMoveHelperProcess(t *testing.T) {
	if os.Getenv(taskMoveHelperEnv) != "1" {
		return
	}

	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator == -1 {
		t.Fatal("helper invocation is missing -- separator")
	}
	os.Args = append([]string{"dharma"}, os.Args[separator+1:]...)

	status, err := strconv.Atoi(os.Getenv("DHARMA_TASK_MOVE_STATUS"))
	if err != nil {
		t.Fatalf("invalid helper response status: %v", err)
	}
	capturePath := os.Getenv("DHARMA_TASK_MOVE_CAPTURE")
	http.DefaultTransport = taskMoveRoundTripper(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		record, err := json.Marshal(taskMoveRequest{Method: req.Method, Path: req.URL.Path, Body: body})
		if err != nil {
			return nil, err
		}
		capture, err := os.OpenFile(capturePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		if _, err := fmt.Fprintln(capture, string(record)); err != nil {
			capture.Close()
			return nil, err
		}
		if err := capture.Close(); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(os.Getenv("DHARMA_TASK_MOVE_RESPONSE"))),
			Request:    req,
		}, nil
	})

	Execute("test")
	// Execute exits on errors; explicitly exit on success too so the Go test
	// runner does not append its own PASS line to the CLI's captured stdout.
	os.Exit(0)
}

func runTaskMoveCLI(t *testing.T, status int, response string, args ...string) taskMoveResult {
	t.Helper()
	capturePath := t.TempDir() + "/requests.jsonl"
	cmdArgs := append([]string{"-test.run=^TestTaskMoveHelperProcess$", "--"}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Env = append(os.Environ(),
		taskMoveHelperEnv+"=1",
		"ASANA_TOKEN=test-token",
		"DHARMA_TASK_MOVE_CAPTURE="+capturePath,
		"DHARMA_TASK_MOVE_STATUS="+strconv.Itoa(status),
		"DHARMA_TASK_MOVE_RESPONSE="+response,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	exitCode := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running helper: %v", err)
		}
		exitCode = exitErr.ExitCode()
	}

	var requests []taskMoveRequest
	if raw, err := os.ReadFile(capturePath); err == nil {
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			var request taskMoveRequest
			if err := json.Unmarshal(line, &request); err != nil {
				t.Fatalf("decoding captured request %q: %v", line, err)
			}
			requests = append(requests, request)
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("reading captured requests: %v", err)
	}

	return taskMoveResult{ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String(), Requests: requests}
}

func TestTaskMovePlacementRequests(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantBody string
	}{
		{
			name:     "before",
			args:     []string{"task", "move", "T", "--section", "S", "--before", "A"},
			wantBody: `{"data":{"insert_before":"A","task":"T"}}`,
		},
		{
			name:     "after",
			args:     []string{"task", "move", "T", "--section", "S", "--after", "A"},
			wantBody: `{"data":{"insert_after":"A","task":"T"}}`,
		},
		{
			name:     "unanchored",
			args:     []string{"task", "move", "T", "--section", "S"},
			wantBody: `{"data":{"task":"T"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runTaskMoveCLI(t, http.StatusOK, `{"data":{"gid":"T"}}`, tt.args...)
			if result.ExitCode != 0 {
				t.Fatalf("exit code = %d, want 0; stderr=%q stdout=%q", result.ExitCode, result.Stderr, result.Stdout)
			}
			if result.Stderr != "" {
				t.Errorf("stderr = %q, want empty", result.Stderr)
			}
			if result.Stdout != "{\"ok\":true,\"data\":{\"gid\":\"T\"}}\n" {
				t.Errorf("stdout = %q", result.Stdout)
			}
			if len(result.Requests) != 1 {
				t.Fatalf("request count = %d, want 1", len(result.Requests))
			}
			request := result.Requests[0]
			if request.Method != http.MethodPost {
				t.Errorf("method = %q, want POST", request.Method)
			}
			if request.Path != "/api/1.0/sections/S/addTask" {
				t.Errorf("path = %q", request.Path)
			}
			if string(request.Body) != tt.wantBody {
				t.Errorf("body = %s, want %s", request.Body, tt.wantBody)
			}
		})
	}
}

func TestTaskMoveUsageErrorsDoNotRequest(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantMessage string
	}{
		{
			name:        "conflicting anchors",
			args:        []string{"task", "move", "T", "--section", "S", "--before", "A", "--after", "B"},
			wantMessage: "--before and --after are mutually exclusive",
		},
		{
			name:        "conflicting empty anchors",
			args:        []string{"task", "move", "T", "--section", "S", "--before=", "--after="},
			wantMessage: "--before and --after are mutually exclusive",
		},
		{
			name:        "empty before",
			args:        []string{"task", "move", "T", "--section", "S", "--before="},
			wantMessage: "--before must not be empty",
		},
		{
			name:        "empty after",
			args:        []string{"task", "move", "T", "--section", "S", "--after="},
			wantMessage: "--after must not be empty",
		},
		{
			name:        "missing section",
			args:        []string{"task", "move", "T"},
			wantMessage: "--section is required (a section gid)",
		},
		{
			name:        "missing task argument",
			args:        []string{"task", "move", "--section", "S"},
			wantMessage: "accepts 1 arg(s), received 0",
		},
		{
			name:        "too many task arguments",
			args:        []string{"task", "move", "T", "extra", "--section", "S"},
			wantMessage: "accepts 1 arg(s), received 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runTaskMoveCLI(t, http.StatusOK, `{"data":{}}`, tt.args...)
			if result.ExitCode != 3 {
				t.Errorf("exit code = %d, want 3", result.ExitCode)
			}
			if len(result.Requests) != 0 {
				t.Errorf("request count = %d, want 0", len(result.Requests))
			}
			if !strings.Contains(result.Stdout, `"ok":false`) || !strings.Contains(result.Stdout, tt.wantMessage) {
				t.Errorf("stdout = %q, want structured error containing %q", result.Stdout, tt.wantMessage)
			}
			if !strings.Contains(result.Stderr, "Error: "+tt.wantMessage) {
				t.Errorf("stderr = %q, want summary containing %q", result.Stderr, tt.wantMessage)
			}
		})
	}
}

func TestTaskMoveAnchorAPIError(t *testing.T) {
	response := `{"errors":[{"message":"Task is not in the destination section","help":"Choose an anchor in this section"}]}`
	result := runTaskMoveCLI(t, http.StatusBadRequest, response,
		"task", "move", "T", "--section", "S", "--before", "A")

	if result.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", result.ExitCode)
	}
	if len(result.Requests) != 1 {
		t.Fatalf("request count = %d, want 1 (no fallback move)", len(result.Requests))
	}
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Message    string `json:"message"`
			HTTPStatus int    `json:"http_status"`
			Help       string `json:"help"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("decoding stdout %q: %v", result.Stdout, err)
	}
	if envelope.OK {
		t.Error("ok = true, want false")
	}
	if envelope.Error.Message != "Task is not in the destination section" {
		t.Errorf("message = %q", envelope.Error.Message)
	}
	if envelope.Error.HTTPStatus != http.StatusBadRequest {
		t.Errorf("http_status = %d, want 400", envelope.Error.HTTPStatus)
	}
	if envelope.Error.Help != "Choose an anchor in this section" {
		t.Errorf("help = %q", envelope.Error.Help)
	}
	if !strings.Contains(result.Stderr, "Error: Task is not in the destination section") {
		t.Errorf("stderr = %q", result.Stderr)
	}
}
