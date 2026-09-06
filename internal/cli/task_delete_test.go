package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jpaddison3/dharma/internal/client"
)

func captureTaskDeleteStdout(t *testing.T, fn func() error) ([]byte, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	runErr := fn()
	os.Stdout = orig
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b, readErr := io.ReadAll(r)
	if closeErr := r.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	return b, runErr
}

func newTaskDeleteTestClient(server *httptest.Server) *client.Client {
	c := client.New("test-token")
	c.BaseURL = server.URL
	return c
}

func assertTaskDeleteRequest(t *testing.T, r *http.Request, wantGID string) {
	t.Helper()
	if r.Method != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", r.Method)
	}
	if r.URL.Path != "/tasks/"+wantGID {
		t.Errorf("path = %q, want /tasks/%s", r.URL.Path, wantGID)
	}
	if r.URL.RawQuery != "" {
		t.Errorf("query = %q, want none", r.URL.RawQuery)
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 0 {
		t.Errorf("body = %q, want empty", body)
	}
}

func decodeTaskDeleteEnvelope(t *testing.T, b []byte) map[string]interface{} {
	t.Helper()
	var envelope map[string]interface{}
	if err := json.Unmarshal(b, &envelope); err != nil {
		t.Fatalf("decode output %q: %v", b, err)
	}
	return envelope
}

func TestRunTaskDeleteSuccess(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantData   interface{}
	}{
		{name: "data object", statusCode: http.StatusOK, response: `{"data":{}}`, wantData: map[string]interface{}{}},
		{name: "zero-byte 200", statusCode: http.StatusOK, wantData: nil},
		{name: "zero-byte 204", statusCode: http.StatusNoContent, wantData: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				assertTaskDeleteRequest(t, r, "123")
				w.WriteHeader(tt.statusCode)
				if tt.response != "" {
					_, _ = io.WriteString(w, tt.response)
				}
			}))
			defer server.Close()

			stdout, err := captureTaskDeleteStdout(t, func() error {
				return runTaskDelete(context.Background(), newTaskDeleteTestClient(server), "123")
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := requests.Load(); got != 1 {
				t.Fatalf("requests = %d, want 1", got)
			}
			envelope := decodeTaskDeleteEnvelope(t, stdout)
			if envelope["ok"] != true {
				t.Errorf("ok = %v, want true", envelope["ok"])
			}
			gotData := envelope["data"]
			if tt.wantData == nil {
				if gotData != nil {
					t.Errorf("data = %#v, want nil", gotData)
				}
			} else if gotMap, ok := gotData.(map[string]interface{}); !ok || len(gotMap) != 0 {
				t.Errorf("data = %#v, want empty object", gotData)
			}
		})
	}
}

func TestRunTaskDeleteMalformedJSON(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		assertTaskDeleteRequest(t, r, "bad-json")
		_, _ = io.WriteString(w, `{"data":`)
	}))
	defer server.Close()

	stdout, err := captureTaskDeleteStdout(t, func() error {
		return runTaskDelete(context.Background(), newTaskDeleteTestClient(server), "bad-json")
	})
	if err == nil || !strings.Contains(err.Error(), "decoding response") {
		t.Fatalf("error = %v, want response decoding error", err)
	}
	if len(stdout) != 0 {
		t.Errorf("stdout = %q, want no success output", stdout)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
}

func TestRunTaskDeleteAPIErrors(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				assertTaskDeleteRequest(t, r, "missing")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"errors":[{"message":"delete failed","help":"check access"}]}`)
			}))
			defer server.Close()

			stdout, err := captureTaskDeleteStdout(t, func() error {
				return runTaskDelete(context.Background(), newTaskDeleteTestClient(server), "missing")
			})
			if len(stdout) != 0 {
				t.Errorf("stdout = %q, want no success output", stdout)
			}
			var apiErr *client.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %T(%v), want *client.APIError", err, err)
			}
			if apiErr.StatusCode != status || apiErr.Message() != "delete failed" || apiErr.HelpText() != "check access" {
				t.Errorf("API error = %#v, want status=%d message/help preserved", apiErr, status)
			}
			if got := requests.Load(); got != 1 {
				t.Errorf("requests = %d, want 1", got)
			}
		})
	}
}

func TestRunTaskDeleteDroppedConnectionIsNotRetried(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		assertTaskDeleteRequest(t, r, "ambiguous")
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server does not support hijacking")
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	}))
	defer server.Close()

	stdout, err := captureTaskDeleteStdout(t, func() error {
		return runTaskDelete(context.Background(), newTaskDeleteTestClient(server), "ambiguous")
	})
	if err == nil {
		t.Fatal("expected dropped connection to fail")
	}
	if len(stdout) != 0 {
		t.Errorf("stdout = %q, want no success output", stdout)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("requests = %d, want 1 (no automatic retry)", got)
	}
}

func TestTaskDeleteCommandRegistered(t *testing.T) {
	cmd, _, err := taskCmd.Find([]string{"delete"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != taskDeleteCmd {
		t.Fatalf("task delete resolves to %p, want %p", cmd, taskDeleteCmd)
	}
}

type taskDeleteRoundTripper func(*http.Request) (*http.Response, error)

func (fn taskDeleteRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestTaskDeleteCLIHelper(t *testing.T) {
	if os.Getenv("DHARMA_TASK_DELETE_HELPER") != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("DHARMA_TASK_DELETE_ARGS")), &args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(91)
	}

	mode := os.Getenv("DHARMA_TASK_DELETE_MODE")
	var calls int
	http.DefaultTransport = taskDeleteRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodDelete || req.URL.Path != "/api/1.0/tasks/123" || req.URL.RawQuery != "" || req.Body != nil {
			return nil, fmt.Errorf("unexpected request: %s %s body=%v", req.Method, req.URL.String(), req.Body)
		}
		if mode == "drop" {
			return nil, errors.New("connection dropped")
		}
		status := http.StatusOK
		body := `{"data":{}}`
		switch mode {
		case "malformed":
			body = `{"data":`
		case "401", "403", "404", "429", "500":
			_, _ = fmt.Sscan(mode, &status)
			body = `{"errors":[{"message":"delete failed","help":"check access"}]}`
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	rootCmd.SetArgs(args)
	Execute("test")
	if strings.HasPrefix(mode, "success") && calls != 1 {
		fmt.Fprintf(os.Stderr, "requests = %d, want 1\n", calls)
		os.Exit(92)
	}
	os.Exit(0)
}

type taskDeleteCLIResult struct {
	stdout string
	stderr string
	code   int
}

func runTaskDeleteCLI(t *testing.T, mode string, args ...string) taskDeleteCLIResult {
	t.Helper()
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTaskDeleteCLIHelper$")
	cmd.Env = append(os.Environ(),
		"DHARMA_TASK_DELETE_HELPER=1",
		"DHARMA_TASK_DELETE_ARGS="+string(encodedArgs),
		"DHARMA_TASK_DELETE_MODE="+mode,
		"ASANA_TOKEN=test-token",
		"XDG_CONFIG_HOME="+t.TempDir(),
	)
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = stdinR
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = stdinR.Close()
	waitErr := cmd.Wait()
	_ = stdinW.Close()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatal("CLI timed out, possibly waiting for stdin")
	}
	code := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			t.Fatal(waitErr)
		}
		code = exitErr.ExitCode()
	}
	return taskDeleteCLIResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func TestTaskDeleteCLIHelp(t *testing.T) {
	result := runTaskDeleteCLI(t, "help", "task", "delete", "--help")
	if result.code != 0 {
		t.Fatalf("exit = %d, stderr = %q", result.code, result.stderr)
	}
	for _, want := range []string{"Immediately delete one task", "Usage:", "dharma task delete <gid>", "not retried automatically"} {
		if !strings.Contains(result.stdout, want) {
			t.Errorf("help does not contain %q:\n%s", want, result.stdout)
		}
	}
}

func TestTaskDeleteCLIValidNoninteractiveInvocation(t *testing.T) {
	result := runTaskDeleteCLI(t, "success", "task", "delete", "123")
	if result.code != 0 || result.stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", result.code, result.stderr)
	}
	envelope := decodeTaskDeleteEnvelope(t, []byte(result.stdout))
	if envelope["ok"] != true {
		t.Errorf("output = %s, want ok:true", result.stdout)
	}
	if data, ok := envelope["data"].(map[string]interface{}); !ok || len(data) != 0 {
		t.Errorf("data = %#v, want empty object", envelope["data"])
	}
}

func TestTaskDeleteCLIInheritsOutputAndVerboseFlags(t *testing.T) {
	result := runTaskDeleteCLI(t, "success", "--output", "toon", "--verbose", "task", "delete", "123")
	if result.code != 0 {
		t.Fatalf("exit = %d, stderr = %q", result.code, result.stderr)
	}
	for _, want := range []string{"data:", "ok: true"} {
		if !strings.Contains(result.stdout, want) {
			t.Errorf("TOON output does not contain %q:\n%s", want, result.stdout)
		}
	}
	for _, want := range []string{"> DELETE https://app.asana.com/api/1.0/tasks/123", "< 200"} {
		if !strings.Contains(result.stderr, want) {
			t.Errorf("verbose stderr does not contain %q:\n%s", want, result.stderr)
		}
	}
}

func TestTaskDeleteCLIWrongArgumentCounts(t *testing.T) {
	for _, args := range [][]string{
		{"task", "delete"},
		{"task", "delete", "123", "456"},
	} {
		result := runTaskDeleteCLI(t, "success", args...)
		if result.code != 3 {
			t.Errorf("args %v: exit = %d, want 3; stderr = %q", args, result.code, result.stderr)
		}
		assertTaskDeleteCLIErrorEnvelope(t, result, 0, "accepts 1 arg")
	}
}

func TestTaskDeleteCLIErrorExitCodesAndEnvelopes(t *testing.T) {
	tests := []struct {
		mode       string
		wantCode   int
		wantStatus int
		wantMsg    string
	}{
		{mode: "401", wantCode: 2, wantStatus: 401, wantMsg: "delete failed"},
		{mode: "403", wantCode: 1, wantStatus: 403, wantMsg: "delete failed"},
		{mode: "404", wantCode: 1, wantStatus: 404, wantMsg: "delete failed"},
		{mode: "429", wantCode: 1, wantStatus: 429, wantMsg: "delete failed"},
		{mode: "500", wantCode: 1, wantStatus: 500, wantMsg: "delete failed"},
		{mode: "malformed", wantCode: 1, wantMsg: "decoding response"},
		{mode: "drop", wantCode: 1, wantMsg: "connection dropped"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			result := runTaskDeleteCLI(t, tt.mode, "task", "delete", "123")
			if result.code != tt.wantCode {
				t.Fatalf("exit = %d, want %d; stderr = %q", result.code, tt.wantCode, result.stderr)
			}
			assertTaskDeleteCLIErrorEnvelope(t, result, tt.wantStatus, tt.wantMsg)
		})
	}
}

func TestTaskDeleteCLIMissingToken(t *testing.T) {
	encodedArgs, err := json.Marshal([]string{"task", "delete", "123"})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestTaskDeleteCLIHelper$")
	cmd.Env = append(os.Environ(),
		"DHARMA_TASK_DELETE_HELPER=1",
		"DHARMA_TASK_DELETE_ARGS="+string(encodedArgs),
		"DHARMA_TASK_DELETE_MODE=success",
		"ASANA_TOKEN=",
		"XDG_CONFIG_HOME="+t.TempDir(),
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Fatalf("error = %v, want exit 2; stderr = %q", err, stderr.String())
	}
	assertTaskDeleteCLIErrorEnvelope(t, taskDeleteCLIResult{stdout: stdout.String(), stderr: stderr.String(), code: 2}, 0, "no token found")
}

func assertTaskDeleteCLIErrorEnvelope(t *testing.T, result taskDeleteCLIResult, wantStatus int, wantMessage string) {
	t.Helper()
	envelope := decodeTaskDeleteEnvelope(t, []byte(result.stdout))
	if envelope["ok"] != false {
		t.Errorf("ok = %v, want false", envelope["ok"])
	}
	errorValue, ok := envelope["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error = %#v, want object", envelope["error"])
	}
	message, _ := errorValue["message"].(string)
	if !strings.Contains(message, wantMessage) {
		t.Errorf("message = %q, want it to contain %q", message, wantMessage)
	}
	if wantStatus == 0 {
		if _, present := errorValue["http_status"]; present {
			t.Errorf("http_status = %v, want omitted", errorValue["http_status"])
		}
		if _, present := errorValue["help"]; present {
			t.Errorf("help = %v, want omitted", errorValue["help"])
		}
	} else if errorValue["http_status"] != float64(wantStatus) {
		t.Errorf("http_status = %v, want %d", errorValue["http_status"], wantStatus)
	} else if errorValue["help"] != "check access" {
		t.Errorf("help = %v, want check access", errorValue["help"])
	}
	if strings.HasPrefix(result.stderr, "Error: ") == false {
		t.Errorf("stderr = %q, want Error summary", result.stderr)
	}
}
