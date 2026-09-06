package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jpaddison3/dharma/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type taskWriteRequest struct {
	method string
	path   string
	data   map[string]interface{}
}

func captureTaskWriteRequests(t *testing.T, mutationStatus int) *[]taskWriteRequest {
	t.Helper()
	original := http.DefaultTransport
	requests := []taskWriteRequest{}
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured := taskWriteRequest{method: req.Method, path: req.URL.Path}
		if req.Body != nil {
			var envelope struct {
				Data map[string]interface{} `json:"data"`
			}
			if err := json.NewDecoder(req.Body).Decode(&envelope); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			captured.data = envelope.Data
		}
		requests = append(requests, captured)

		status := mutationStatus
		response := `{"data":{"gid":"returned"}}`
		if req.Method == http.MethodGet && req.URL.Path == "/api/1.0/workspaces" {
			status = http.StatusOK
			response = `{"data":[{"gid":"workspace-from-api","name":"Only workspace"}]}`
		} else if status >= http.StatusBadRequest {
			response = `{"errors":[{"message":"stub failure"}]}`
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(response)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	return &requests
}

func runTaskWriteCommand(t *testing.T, cmd *cobra.Command, argv []string, workspace string) (string, string, error) {
	t.Helper()
	// These commands use package globals bound to persistent Cobra flags. Keep
	// every mutation local because this test file deliberately cannot run in
	// parallel.
	resetTaskWriteCommandState()
	originalToken, originalWorkspace, originalFormat, originalCommandRan := flagToken, flagWorkspace, output.Format, commandRan
	flagToken, flagWorkspace, output.Format = "dummy-token", workspace, "json"
	t.Cleanup(func() {
		resetTaskWriteCommandState()
		flagToken, flagWorkspace, output.Format = originalToken, originalWorkspace, originalFormat
		commandRan = originalCommandRan
	})

	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	t.Cleanup(func() { cmd.SetErr(nil) })
	if err := cmd.Flags().Parse(argv); err != nil {
		return "", stderr.String(), err
	}
	args := cmd.Flags().Args()
	if cmd.Args != nil {
		if err := cmd.Args(cmd, args); err != nil {
			return "", stderr.String(), err
		}
	}

	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = stdout
	defer func() { os.Stdout = originalStdout }()

	err = cmd.RunE(cmd, args)
	if _, seekErr := stdout.Seek(0, 0); seekErr != nil {
		t.Fatal(seekErr)
	}
	b, readErr := io.ReadAll(stdout)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(b), stderr.String(), err
}

func resetTaskWriteCommandState() {
	taskCreateName = ""
	taskCreateProjects = nil
	taskCreateNotes = ""
	taskCreateHTMLNotes = ""
	taskCreateAssignee = ""
	taskCommentText = ""
	taskCommentHTMLText = ""
	taskSetNotesText = ""
	taskSetHTMLNotes = ""
	for _, cmd := range []*cobra.Command{taskCreateCmd, taskCommentCmd, taskSetNotesCmd} {
		cmd.Flags().VisitAll(func(flag *pflag.Flag) { flag.Changed = false })
	}
}

func TestTaskHTMLWrites(t *testing.T) {
	base := `<body>It's "quoted"` + "\n" + `café → next &amp; <a data-asana-gid="123"/></body>`
	for _, command := range []string{"create", "set-notes", "comment"} {
		for _, source := range []string{"literal", "file", "stdin"} {
			t.Run(command+" "+source, func(t *testing.T) {
				requests := captureTaskWriteRequests(t, http.StatusOK)
				spec, want := base, base
				switch source {
				case "file":
					path := filepath.Join(t.TempDir(), "rich.html")
					if err := os.WriteFile(path, []byte(base+"\n\n"), 0644); err != nil {
						t.Fatal(err)
					}
					spec, want = "@"+path, base+"\n"
				case "stdin":
					withStdin(t, base+"\n\n")
					spec, want = "@-", base+"\n"
				}

				var cmd *cobra.Command
				var argv []string
				var wantMethod, wantPath, wantHTMLKey, wantPlainKey string
				switch command {
				case "create":
					cmd = taskCreateCmd
					argv = []string{"--name", "Task", "--project", "42", "--html-notes", spec}
					wantMethod, wantPath, wantHTMLKey, wantPlainKey = http.MethodPost, "/api/1.0/tasks", "html_notes", "notes"
				case "set-notes":
					cmd = taskSetNotesCmd
					argv = []string{"--html-notes", spec, "7"}
					wantMethod, wantPath, wantHTMLKey, wantPlainKey = http.MethodPut, "/api/1.0/tasks/7", "html_notes", "notes"
				case "comment":
					cmd = taskCommentCmd
					argv = []string{"--html-text", spec, "7"}
					wantMethod, wantPath, wantHTMLKey, wantPlainKey = http.MethodPost, "/api/1.0/tasks/7/stories", "html_text", "text"
				}

				stdout, _, err := runTaskWriteCommand(t, cmd, argv, "configured-workspace")
				if err != nil {
					t.Fatal(err)
				}
				if len(*requests) != 1 {
					t.Fatalf("requests = %d, want 1", len(*requests))
				}
				req := (*requests)[0]
				if req.method != wantMethod || req.path != wantPath {
					t.Errorf("request = %s %s, want %s %s", req.method, req.path, wantMethod, wantPath)
				}
				if got := req.data[wantHTMLKey]; got != want {
					t.Errorf("%s = %q, want %q", wantHTMLKey, got, want)
				}
				if _, present := req.data[wantPlainKey]; present {
					t.Errorf("plain field %q unexpectedly present in %#v", wantPlainKey, req.data)
				}
				var envelope map[string]interface{}
				if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
					t.Fatalf("stdout is not JSON: %q: %v", stdout, err)
				}
				if envelope["ok"] != true {
					t.Errorf("stdout = %s, want ok:true", stdout)
				}
			})
		}
	}
}

func TestTaskHTMLNumericReferenceWarningDoesNotRewrite(t *testing.T) {
	requests := captureTaskWriteRequests(t, http.StatusOK)
	want := "<body>It&#39;s café</body>"
	_, stderr, err := runTaskWriteCommand(t, taskSetNotesCmd, []string{"--html-notes", want, "7"}, "ws")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "numeric character references are stored literally") {
		t.Errorf("stderr = %q, want numeric-reference warning", stderr)
	}
	if got := (*requests)[0].data["html_notes"]; got != want {
		t.Errorf("request html_notes = %q, want unchanged %q", got, want)
	}
}

func TestTaskWriteValidationBeforeInputOrHTTP(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.html")
	tests := []struct {
		name string
		cmd  *cobra.Command
		argv []string
	}{
		{"create conflict including empty plain", taskCreateCmd, []string{"--name", "Task", "--notes", "", "--html-notes", "@-"}},
		{"set-notes conflict including empty plain", taskSetNotesCmd, []string{"--notes", "", "--html-notes", "@-", "7"}},
		{"comment conflict including empty plain", taskCommentCmd, []string{"--text", "", "--html-text", "@-", "7"}},
		{"set-notes missing description", taskSetNotesCmd, []string{"7"}},
		{"create empty html", taskCreateCmd, []string{"--name", "Task", "--html-notes", ""}},
		{"set-notes unreadable html file", taskSetNotesCmd, []string{"--html-notes", "@" + missing, "7"}},
		{"comment empty explicit text", taskCommentCmd, []string{"--text", "", "7"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := captureTaskWriteRequests(t, http.StatusOK)
			withStdin(t, "must not be read")
			_, _, err := runTaskWriteCommand(t, tt.cmd, tt.argv, "ws")
			if err == nil {
				t.Fatal("expected validation error")
			}
			commandRan = true
			if code, _ := classifyError(err); code != 3 {
				t.Errorf("classifyError = %d, want usage exit 3 (%v)", code, err)
			}
			if len(*requests) != 0 {
				t.Errorf("validation issued %d HTTP request(s)", len(*requests))
			}
			if strings.Contains(tt.name, "conflict") && stdinConsumed {
				t.Error("conflict consumed stdin")
			}
		})
	}
}

func TestTaskWriteEmptyExpandedHTML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.html")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		spec string
	}{
		{"empty file", "@" + path},
		{"empty stdin", "@-"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := captureTaskWriteRequests(t, http.StatusOK)
			if tc.spec == "@-" {
				withStdin(t, "")
			}
			_, _, err := runTaskWriteCommand(t, taskCommentCmd, []string{"--html-text", tc.spec, "7"}, "ws")
			var usageErr *UsageError
			if !errors.As(err, &usageErr) {
				t.Fatalf("error = %T(%v), want *UsageError", err, err)
			}
			if len(*requests) != 0 {
				t.Errorf("empty HTML issued %d HTTP request(s)", len(*requests))
			}
		})
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestTaskWriteReaderAndAPIErrorsAreOperational(t *testing.T) {
	t.Run("stdin read failure", func(t *testing.T) {
		requests := captureTaskWriteRequests(t, http.StatusOK)
		originalReader, originalConsumed := stdinReader, stdinConsumed
		stdinReader, stdinConsumed = failingReader{}, false
		t.Cleanup(func() { stdinReader, stdinConsumed = originalReader, originalConsumed })
		_, _, err := runTaskWriteCommand(t, taskSetNotesCmd, []string{"--html-notes", "@-", "7"}, "ws")
		commandRan = true
		if code, _ := classifyError(err); code != 1 {
			t.Errorf("classifyError = %d, want operational exit 1 (%v)", code, err)
		}
		if len(*requests) != 0 {
			t.Errorf("reader failure issued %d HTTP request(s)", len(*requests))
		}
	})

	t.Run("API failure", func(t *testing.T) {
		requests := captureTaskWriteRequests(t, http.StatusInternalServerError)
		_, _, err := runTaskWriteCommand(t, taskSetNotesCmd, []string{"--html-notes", "<body>ok</body>", "7"}, "ws")
		commandRan = true
		if code, _ := classifyError(err); code != 1 {
			t.Errorf("classifyError = %d, want operational exit 1 (%v)", code, err)
		}
		if len(*requests) != 1 {
			t.Errorf("API failure requests = %d, want 1", len(*requests))
		}
	})
}

func TestTaskPlainTextRegressions(t *testing.T) {
	for _, value := range []string{"@handle", "@-", "<strong>text</strong>"} {
		t.Run("create literal "+value, func(t *testing.T) {
			requests := captureTaskWriteRequests(t, http.StatusOK)
			withStdin(t, "must not be read")
			_, _, err := runTaskWriteCommand(t, taskCreateCmd, []string{"--name", "Task", "--project", "42", "--notes", value}, "ws")
			if err != nil {
				t.Fatal(err)
			}
			if got := (*requests)[0].data["notes"]; got != value {
				t.Errorf("notes = %q, want literal %q", got, value)
			}
			if stdinConsumed {
				t.Error("literal plain text consumed stdin")
			}
		})
	}

	t.Run("empty create notes omitted", func(t *testing.T) {
		requests := captureTaskWriteRequests(t, http.StatusOK)
		_, _, err := runTaskWriteCommand(t, taskCreateCmd, []string{"--name", "Task", "--project", "42", "--notes", ""}, "ws")
		if err != nil {
			t.Fatal(err)
		}
		if _, present := (*requests)[0].data["notes"]; present {
			t.Errorf("empty create notes should be omitted: %#v", (*requests)[0].data)
		}
	})

	t.Run("set-notes clears", func(t *testing.T) {
		requests := captureTaskWriteRequests(t, http.StatusOK)
		_, _, err := runTaskWriteCommand(t, taskSetNotesCmd, []string{"--notes", "", "7"}, "ws")
		if err != nil {
			t.Fatal(err)
		}
		if got, present := (*requests)[0].data["notes"]; !present || got != "" {
			t.Errorf("notes = %q (present=%v), want present empty string", got, present)
		}
	})

	t.Run("comment defaults to stdin", func(t *testing.T) {
		requests := captureTaskWriteRequests(t, http.StatusOK)
		withStdin(t, "plain\ncomment\n")
		_, _, err := runTaskWriteCommand(t, taskCommentCmd, []string{"7"}, "ws")
		if err != nil {
			t.Fatal(err)
		}
		if got := (*requests)[0].data["text"]; got != "plain\ncomment" {
			t.Errorf("text = %q", got)
		}
	})

	t.Run("explicit text bypasses stdin", func(t *testing.T) {
		requests := captureTaskWriteRequests(t, http.StatusOK)
		withStdin(t, "must not be read")
		_, _, err := runTaskWriteCommand(t, taskCommentCmd, []string{"--text", "@-", "7"}, "ws")
		if err != nil {
			t.Fatal(err)
		}
		if got := (*requests)[0].data["text"]; got != "@-" {
			t.Errorf("text = %q, want literal @-", got)
		}
		if stdinConsumed {
			t.Error("explicit --text consumed stdin")
		}
	})
}

func TestTaskCreatePlacement(t *testing.T) {
	t.Setenv("ASANA_WORKSPACE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	t.Run("project avoids workspace discovery", func(t *testing.T) {
		requests := captureTaskWriteRequests(t, http.StatusOK)
		_, _, err := runTaskWriteCommand(t, taskCreateCmd, []string{"--name", "Task", "--project", "1", "--project", "2"}, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(*requests) != 1 || (*requests)[0].path != "/api/1.0/tasks" {
			t.Fatalf("requests = %#v, want only POST /tasks", *requests)
		}
		projects, ok := (*requests)[0].data["projects"].([]interface{})
		if !ok || len(projects) != 2 || projects[0] != "1" || projects[1] != "2" {
			t.Errorf("projects = %#v", (*requests)[0].data["projects"])
		}
	})

	t.Run("workspace fallback is preserved", func(t *testing.T) {
		requests := captureTaskWriteRequests(t, http.StatusOK)
		_, _, err := runTaskWriteCommand(t, taskCreateCmd, []string{"--name", "Task"}, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(*requests) != 2 || (*requests)[0].path != "/api/1.0/workspaces" || (*requests)[1].path != "/api/1.0/tasks" {
			t.Fatalf("requests = %#v, want workspace GET then task POST", *requests)
		}
		if got := (*requests)[1].data["workspace"]; got != "workspace-from-api" {
			t.Errorf("workspace = %q", got)
		}
	})
}
