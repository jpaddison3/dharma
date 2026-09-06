package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jpaddison3/dharma/internal/client"
	"github.com/jpaddison3/dharma/internal/output"
)

type capturedSectionRequest struct {
	method        string
	path          string
	authorization string
	query         url.Values
	body          string
}

type sectionCommandResult struct {
	err      error
	exitCode int
	payload  errorPayload
	stdout   []byte
	requests []capturedSectionRequest
}

// runSectionCommand exercises the globally registered Cobra command while
// isolating the process-wide state used by the production client and output.
func runSectionCommand(t *testing.T, args []string, status int, response string) sectionCommandResult {
	t.Helper()

	origTransport := http.DefaultTransport
	origStdout := os.Stdout
	origToken, origWorkspace := flagToken, flagWorkspace
	origVerbose, origOutput := flagVerbose, flagOutput
	origFormat := output.Format
	origCommandRan := commandRan
	origFields := sectionGetFields
	fieldsFlag := sectionGetCmd.Flags().Lookup("fields")
	origFieldsChanged := fieldsFlag.Changed
	defer func() {
		http.DefaultTransport = origTransport
		os.Stdout = origStdout
		flagToken, flagWorkspace = origToken, origWorkspace
		flagVerbose, flagOutput = origVerbose, origOutput
		output.Format = origFormat
		commandRan = origCommandRan
		sectionGetFields = origFields
		fieldsFlag.Changed = origFieldsChanged
		rootCmd.SetArgs(nil)
	}()

	flagToken = "test-token"
	flagWorkspace = ""
	flagVerbose = false
	flagOutput = "json"
	output.Format = "json"
	commandRan = false
	sectionGetFields = fieldsFlag.DefValue
	fieldsFlag.Changed = false

	var requests []capturedSectionRequest
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body []byte
		if req.Body != nil {
			var err error
			body, err = io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("reading request body: %v", err)
			}
		}
		requests = append(requests, capturedSectionRequest{
			method:        req.Method,
			path:          req.URL.Path,
			authorization: req.Header.Get("Authorization"),
			query:         req.URL.Query(),
			body:          string(body),
		})
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(response)),
			Request:    req,
		}, nil
	})

	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writePipe
	rootCmd.SetArgs(args)
	runErr := rootCmd.Execute()
	os.Stdout = origStdout
	if err := writePipe.Close(); err != nil {
		t.Fatal(err)
	}
	stdout, readErr := io.ReadAll(readPipe)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err := readPipe.Close(); err != nil {
		t.Fatal(err)
	}

	result := sectionCommandResult{err: runErr, stdout: stdout, requests: requests}
	if runErr != nil {
		result.exitCode, result.payload = classifyError(runErr)
	}
	return result
}

func TestSectionGetRequestsAndOutput(t *testing.T) {
	const exactName = "Planning \"alpha\"\nLíneas $(echo nope) $HOME"
	tests := []struct {
		name          string
		args          []string
		response      string
		wantFields    string
		wantNoFields  bool
		wantProject   bool
		wantExactName bool
	}{
		{
			name:          "default fields",
			args:          []string{"section", "get", "123"},
			response:      `{"data":{"gid":"123","name":"Planning \"alpha\"\nLíneas $(echo nope) $HOME"}}`,
			wantFields:    "name",
			wantExactName: true,
		},
		{
			name:        "nested project fields",
			args:        []string{"section", "get", "123", "--fields", "name,project.name"},
			response:    `{"data":{"gid":"123","name":"Planning","project":{"gid":"456","name":"Roadmap"}}}`,
			wantFields:  "name,project.name",
			wantProject: true,
		},
		{
			name:         "empty fields",
			args:         []string{"section", "get", "123", "--fields", ""},
			response:     `{"data":{"gid":"123","name":"Planning"}}`,
			wantNoFields: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runSectionCommand(t, tt.args, http.StatusOK, tt.response)
			if got.err != nil {
				t.Fatalf("Execute() error = %v", got.err)
			}
			if len(got.requests) != 1 {
				t.Fatalf("request count = %d, want 1", len(got.requests))
			}
			req := got.requests[0]
			if req.method != http.MethodGet {
				t.Errorf("method = %q, want GET", req.method)
			}
			if req.path != "/api/1.0/sections/123" {
				t.Errorf("path = %q", req.path)
			}
			if req.authorization != "Bearer test-token" {
				t.Errorf("authorization = %q", req.authorization)
			}
			if req.body != "" {
				t.Errorf("body = %q, want empty", req.body)
			}
			if tt.wantNoFields {
				if len(req.query) != 0 {
					t.Errorf("query = %#v, want no parameters", req.query)
				}
			} else {
				if got := req.query.Get("opt_fields"); got != tt.wantFields {
					t.Errorf("opt_fields = %q, want %q", got, tt.wantFields)
				}
				if len(req.query) != 1 {
					t.Errorf("query = %#v, want only opt_fields", req.query)
				}
			}

			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(got.stdout, &envelope); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, got.stdout)
			}
			if len(envelope) != 2 {
				t.Errorf("envelope keys = %v, want only ok and data", envelope)
			}
			var ok bool
			if err := json.Unmarshal(envelope["ok"], &ok); err != nil || !ok {
				t.Errorf("ok = %s, error = %v", envelope["ok"], err)
			}
			var data struct {
				GID     string `json:"gid"`
				Name    string `json:"name"`
				Project *struct {
					GID  string `json:"gid"`
					Name string `json:"name"`
				} `json:"project"`
			}
			if err := json.Unmarshal(envelope["data"], &data); err != nil {
				t.Fatal(err)
			}
			if data.GID != "123" {
				t.Errorf("data.gid = %q", data.GID)
			}
			if tt.wantExactName && data.Name != exactName {
				t.Errorf("data.name = %q, want exact round trip %q", data.Name, exactName)
			}
			if tt.wantProject {
				if data.Project == nil || data.Project.GID != "456" || data.Project.Name != "Roadmap" {
					t.Errorf("data.project = %#v", data.Project)
				}
			}
		})
	}
}

func TestSectionGetAPIErrors(t *testing.T) {
	tests := []struct {
		status   int
		message  string
		help     string
		wantCode int
	}{
		{http.StatusUnauthorized, "Not Authorized", "Use a valid token", 2},
		{http.StatusForbidden, "Forbidden", "Ask for access", 1},
		{http.StatusNotFound, "Section not found", "Check the gid", 1},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			response, err := json.Marshal(map[string]interface{}{
				"errors": []map[string]string{{"message": tt.message, "help": tt.help}},
			})
			if err != nil {
				t.Fatal(err)
			}
			got := runSectionCommand(t, []string{"section", "get", "123"}, tt.status, string(response))
			if got.err == nil {
				t.Fatal("Execute() error = nil")
			}
			if len(got.stdout) != 0 {
				t.Errorf("stdout = %q, want no success output", got.stdout)
			}
			if len(got.requests) != 1 {
				t.Fatalf("request count = %d, want 1", len(got.requests))
			}
			var apiErr *client.APIError
			if !errors.As(got.err, &apiErr) {
				t.Fatalf("error = %T(%v), want *client.APIError", got.err, got.err)
			}
			if apiErr.StatusCode != tt.status || apiErr.Message() != tt.message || apiErr.HelpText() != tt.help {
				t.Errorf("API error = status %d, message %q, help %q", apiErr.StatusCode, apiErr.Message(), apiErr.HelpText())
			}
			if got.exitCode != tt.wantCode {
				t.Errorf("exit code = %d, want %d", got.exitCode, tt.wantCode)
			}
			if got.payload.HTTPStatus != tt.status || got.payload.Message != tt.message || got.payload.Help != tt.help {
				t.Errorf("payload = %#v", got.payload)
			}
		})
	}
}

func TestSectionGetRequiresExactlyOneGID(t *testing.T) {
	for _, args := range [][]string{
		{"section", "get"},
		{"section", "get", "123", "456"},
	} {
		got := runSectionCommand(t, args, http.StatusOK, `{"data":{}}`)
		if got.err == nil {
			t.Fatalf("args %v: Execute() error = nil", args)
		}
		if got.exitCode != 3 {
			t.Errorf("args %v: exit code = %d, want 3", args, got.exitCode)
		}
		if len(got.stdout) != 0 {
			t.Errorf("args %v: stdout = %q, want empty", args, got.stdout)
		}
		if len(got.requests) != 0 {
			t.Errorf("args %v: request count = %d, want 0", args, len(got.requests))
		}
	}
}
