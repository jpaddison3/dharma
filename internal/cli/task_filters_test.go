package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type taskFilterRoundTripFunc func(*http.Request) (*http.Response, error)

func (f taskFilterRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type capturedTaskRequest struct {
	path  string
	query url.Values
}

type taskResponse struct {
	status int
	body   string
}

func resetCommandFlags(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if err := flag.Value.Set(flag.DefValue); err != nil {
			t.Fatalf("resetting --%s: %v", flag.Name, err)
		}
		flag.Changed = false
	})
}

// runTaskFilterCommand exercises pflag parsing and the real Cobra command body.
// The commands bind flags to package globals, so every invocation resets both
// values and Changed state and these tests deliberately never run in parallel.
func runTaskFilterCommand(t *testing.T, cmd *cobra.Command, args []string, respond func(int, *http.Request) taskResponse) (map[string]interface{}, []capturedTaskRequest, error) {
	t.Helper()
	resetCommandFlags(t, cmd)
	defer resetCommandFlags(t, cmd)

	t.Setenv("ASANA_TOKEN", "test-token")
	t.Setenv("ASANA_WORKSPACE", "workspace-1")

	var requests []capturedTaskRequest
	previousTransport := http.DefaultTransport
	http.DefaultTransport = taskFilterRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, capturedTaskRequest{path: req.URL.Path, query: req.URL.Query()})
		result := respond(len(requests)-1, req)
		status := result.status
		if status == 0 {
			status = http.StatusOK
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(result.body)),
			Request:    req,
		}, nil
	})
	defer func() { http.DefaultTransport = previousTransport }()

	previousStdout := os.Stdout
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdout
	defer func() {
		os.Stdout = previousStdout
		_ = stdout.Close()
	}()

	if err := cmd.Flags().Parse(args); err != nil {
		return nil, requests, err
	}
	runErr := cmd.RunE(cmd, cmd.Flags().Args())
	if _, err := stdout.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		return nil, requests, runErr
	}
	var envelope map[string]interface{}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decoding stdout %q: %v", raw, err)
	}
	return envelope, requests, runErr
}

func successfulTaskResponse(_ int, _ *http.Request) taskResponse {
	return taskResponse{body: `{"data":[]}`}
}

func requireUsageErrorWithNoRequests(t *testing.T, err error, requests []capturedTaskRequest) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a usage error")
	}
	if len(requests) != 0 {
		t.Fatalf("validation made %d HTTP request(s), want zero", len(requests))
	}
	previousCommandRan := commandRan
	commandRan = true
	defer func() { commandRan = previousCommandRan }()
	code, _ := classifyError(err)
	if code != 3 {
		t.Fatalf("classifyError(%v) = %d, want usage exit 3", err, code)
	}
}

func TestTaskListTimeFilterMappings(t *testing.T) {
	modified := "2026-08-02T14:03:04.123+05:30"
	completed := "2026-07-06T00:00:00Z"
	envelope, requests, err := runTaskFilterCommand(t, taskListCmd, []string{
		"--project", "project-1",
		"--modified-since", modified,
		"--completed-since", completed,
	}, successfulTaskResponse)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	q := requests[0].query
	if requests[0].path != "/api/1.0/tasks" {
		t.Errorf("path = %q", requests[0].path)
	}
	for key, want := range map[string]string{
		"project":         "project-1",
		"modified_since":  modified,
		"completed_since": completed,
		"limit":           "100",
		"opt_fields":      defaultTaskListFields,
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if envelope["ok"] != true || envelope["has_more"] != false {
		t.Errorf("envelope = %#v", envelope)
	}
}

func TestTaskListOmitsTimeFilters(t *testing.T) {
	_, requests, err := runTaskFilterCommand(t, taskListCmd, []string{"--section", "section-1"}, successfulTaskResponse)
	if err != nil {
		t.Fatal(err)
	}
	q := requests[0].query
	for _, key := range []string{"modified_since", "completed_since"} {
		if _, present := q[key]; present {
			t.Errorf("unexpected %s=%q", key, q.Get(key))
		}
	}
}

func TestTaskListRejectsInvalidTimeFiltersBeforeHTTP(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"empty modified", []string{"--project", "p", "--modified-since="}},
		{"date-only modified", []string{"--project", "p", "--modified-since", "2026-07-06"}},
		{"now modified", []string{"--project", "p", "--modified-since", "now"}},
		{"relative modified", []string{"--project", "p", "--modified-since", "yesterday"}},
		{"malformed modified", []string{"--project", "p", "--modified-since", "2026-13-99T00:00:00Z"}},
		{"empty completed", []string{"--project", "p", "--completed-since="}},
		{"date-only completed", []string{"--project", "p", "--completed-since", "2026-07-06"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, requests, err := runTaskFilterCommand(t, taskListCmd, tt.args, successfulTaskResponse)
			requireUsageErrorWithNoRequests(t, err, requests)
		})
	}
}

func TestTaskListIncompleteInteractions(t *testing.T) {
	timestamp := "2026-07-06T00:00:00Z"
	tests := []struct {
		name      string
		args      []string
		wantValue string
		wantUsage bool
	}{
		{"incomplete alone", []string{"--project", "p", "--incomplete"}, "now", false},
		{"matching now", []string{"--project", "p", "--incomplete", "--completed-since", "now"}, "now", false},
		{"conflicting timestamp", []string{"--project", "p", "--incomplete", "--completed-since", timestamp}, "", true},
		{"explicit false", []string{"--project", "p", "--incomplete=false", "--completed-since", timestamp}, timestamp, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, requests, err := runTaskFilterCommand(t, taskListCmd, tt.args, successfulTaskResponse)
			if tt.wantUsage {
				requireUsageErrorWithNoRequests(t, err, requests)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := requests[0].query.Get("completed_since"); got != tt.wantValue {
				t.Errorf("completed_since = %q, want %q", got, tt.wantValue)
			}
		})
	}
}

func TestTaskListPaginationRetainsFilters(t *testing.T) {
	args := []string{
		"--assignee", "me",
		"--modified-since", "2026-07-06T00:00:00Z",
		"--completed-since", "2026-08-06T00:00:00Z",
		"--limit", "1",
		"--fields", "name,modified_at",
		"--paginate",
	}
	envelope, requests, err := runTaskFilterCommand(t, taskListCmd, args, func(index int, _ *http.Request) taskResponse {
		if index == 0 {
			return taskResponse{body: `{"data":[{"gid":"1"}],"next_page":{"offset":"next-token"}}`}
		}
		return taskResponse{body: `{"data":[{"gid":"2"}]}`}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	for i, request := range requests {
		for key, want := range map[string]string{
			"workspace":       "workspace-1",
			"assignee":        "me",
			"modified_since":  "2026-07-06T00:00:00Z",
			"completed_since": "2026-08-06T00:00:00Z",
			"limit":           "1",
			"opt_fields":      "name,modified_at",
		} {
			if got := request.query.Get(key); got != want {
				t.Errorf("request %d %s = %q, want %q", i+1, key, got, want)
			}
		}
	}
	if got := requests[0].query.Get("offset"); got != "" {
		t.Errorf("first offset = %q", got)
	}
	if got := requests[1].query.Get("offset"); got != "next-token" {
		t.Errorf("second offset = %q", got)
	}
	if envelope["count"] != float64(2) || envelope["has_more"] != false {
		t.Errorf("envelope = %#v", envelope)
	}
}

func TestTaskListFirstPageHasMoreHint(t *testing.T) {
	envelope, requests, err := runTaskFilterCommand(t, taskListCmd, []string{
		"--project", "p", "--modified-since", "2026-07-06T00:00:00Z",
	}, func(_ int, _ *http.Request) taskResponse {
		return taskResponse{body: `{"data":[{"gid":"1"}],"next_page":{"offset":"next-token"}}`}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	if envelope["has_more"] != true || !strings.Contains(envelope["hint"].(string), "--paginate") {
		t.Errorf("envelope = %#v", envelope)
	}
}

func TestTaskSearchNewMappingsAndExistingFilters(t *testing.T) {
	after := "2026-07-06T00:00:00.250+05:30"
	before := "2026-09-06T00:00:00Z"
	text := `quotes " and & and $() — café`
	envelope, requests, err := runTaskFilterCommand(t, taskSearchCmd, []string{
		"--text", text,
		"--assignee", "me",
		"--completed=false",
		"--project", "project-1",
		"--section", "section-1",
		"--tag", "tag-1",
		"--modified-since", "legacy-input",
		"--created-after", after,
		"--created-before", before,
		"--sort-by", "created_at",
		"--sort-ascending=false",
		"--limit", "7",
		"--fields", "name,created_at",
	}, successfulTaskResponse)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	if requests[0].path != "/api/1.0/workspaces/workspace-1/tasks/search" {
		t.Errorf("path = %q", requests[0].path)
	}
	for key, want := range map[string]string{
		"text":              text,
		"assignee.any":      "me",
		"completed":         "false",
		"projects.any":      "project-1",
		"sections.any":      "section-1",
		"tags.any":          "tag-1",
		"modified_at.after": "legacy-input",
		"created_at.after":  after,
		"created_at.before": before,
		"sort_by":           "created_at",
		"sort_ascending":    "false",
		"limit":             "7",
		"opt_fields":        "name,created_at",
	} {
		if got := requests[0].query.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if envelope["ok"] != true {
		t.Errorf("envelope = %#v", envelope)
	}
}

func TestTaskSearchTimestampValidationAndOrdering(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"empty after", []string{"--created-after="}},
		{"date-only after", []string{"--created-after", "2026-07-06"}},
		{"relative before", []string{"--created-before", "tomorrow"}},
		{"malformed before", []string{"--created-before", "not-a-date"}},
		{"equal", []string{"--created-after", "2026-07-06T00:00:00Z", "--created-before", "2026-07-06T00:00:00Z"}},
		{"reversed", []string{"--created-after", "2026-07-07T00:00:00Z", "--created-before", "2026-07-06T00:00:00Z"}},
		{"offset-equivalent", []string{"--created-after", "2026-07-06T01:00:00+01:00", "--created-before", "2026-07-06T00:00:00Z"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, requests, err := runTaskFilterCommand(t, taskSearchCmd, tt.args, successfulTaskResponse)
			requireUsageErrorWithNoRequests(t, err, requests)
		})
	}

	_, requests, err := runTaskFilterCommand(t, taskSearchCmd, []string{
		"--created-after", "2026-07-06T01:00:00+01:00",
		"--created-before", "2026-07-06T00:00:01Z",
	}, successfulTaskResponse)
	if err != nil {
		t.Fatalf("valid cross-offset range: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
}

func TestTaskSearchSortValues(t *testing.T) {
	values := []string{"due_date", "created_at", "completed_at", "likes", "relevance", "modified_at"}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			_, requests, err := runTaskFilterCommand(t, taskSearchCmd, []string{"--sort-by", value}, successfulTaskResponse)
			if err != nil {
				t.Fatal(err)
			}
			if got := requests[0].query.Get("sort_by"); got != value {
				t.Errorf("sort_by = %q, want %q", got, value)
			}
		})
	}
	for _, value := range []string{"", "name", "CREATED_AT"} {
		t.Run("invalid "+value, func(t *testing.T) {
			_, requests, err := runTaskFilterCommand(t, taskSearchCmd, []string{"--sort-by=" + value}, successfulTaskResponse)
			requireUsageErrorWithNoRequests(t, err, requests)
		})
	}
}

func TestTaskSearchSortAscendingTriState(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    string
		present bool
	}{
		{"omitted", nil, "", false},
		{"true", []string{"--sort-ascending"}, "true", true},
		{"false", []string{"--sort-ascending=false"}, "false", true},
		{"direction with sort", []string{"--sort-by", "due_date", "--sort-ascending"}, "true", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, requests, err := runTaskFilterCommand(t, taskSearchCmd, tt.args, successfulTaskResponse)
			if err != nil {
				t.Fatal(err)
			}
			values, present := requests[0].query["sort_ascending"]
			if present != tt.present {
				t.Fatalf("sort_ascending present = %v, want %v (query %v)", present, tt.present, requests[0].query)
			}
			if present && values[0] != tt.want {
				t.Errorf("sort_ascending = %q, want %q", values[0], tt.want)
			}
		})
	}
}

func TestTaskSearchFields(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    string
		present bool
	}{
		{"default", nil, defaultTaskListFields, true},
		{"custom", []string{"--fields", "name,due_on"}, "name,due_on", true},
		{"empty", []string{"--fields="}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, requests, err := runTaskFilterCommand(t, taskSearchCmd, tt.args, successfulTaskResponse)
			if err != nil {
				t.Fatal(err)
			}
			values, present := requests[0].query["opt_fields"]
			if present != tt.present {
				t.Fatalf("opt_fields present = %v, want %v", present, tt.present)
			}
			if present && values[0] != tt.want {
				t.Errorf("opt_fields = %q, want %q", values[0], tt.want)
			}
		})
	}
}

func TestTaskSearchCapEnvelope(t *testing.T) {
	for _, count := range []int{2, 3} {
		t.Run(strconv.Itoa(count)+" results", func(t *testing.T) {
			items := make([]map[string]string, count)
			for i := range items {
				items[i] = map[string]string{"gid": strconv.Itoa(i)}
			}
			body, err := json.Marshal(map[string]interface{}{"data": items})
			if err != nil {
				t.Fatal(err)
			}
			envelope, requests, err := runTaskFilterCommand(t, taskSearchCmd, []string{"--limit", "3"}, func(_ int, _ *http.Request) taskResponse {
				return taskResponse{body: string(body)}
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(requests))
			}
			wantMore := count == 3
			if envelope["has_more"] != wantMore {
				t.Errorf("has_more = %v, want %v", envelope["has_more"], wantMore)
			}
			if wantMore {
				hint, _ := envelope["hint"].(string)
				if !strings.Contains(hint, "no pagination") || !strings.Contains(hint, "--created-after/--created-before") {
					t.Errorf("hint = %q", hint)
				}
			} else if _, present := envelope["hint"]; present {
				t.Errorf("unexpected hint = %v", envelope["hint"])
			}
		})
	}
}

func TestTaskSearchAPIAndAuthErrorClassification(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   int
	}{
		{"api", http.StatusInternalServerError, 1},
		{"auth", http.StatusUnauthorized, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, requests, err := runTaskFilterCommand(t, taskSearchCmd, nil, func(_ int, _ *http.Request) taskResponse {
				return taskResponse{status: tt.status, body: `{"errors":[{"message":"test failure"}]}`}
			})
			if err == nil {
				t.Fatal("expected API error")
			}
			if len(requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(requests))
			}
			previousCommandRan := commandRan
			commandRan = true
			code, _ := classifyError(err)
			commandRan = previousCommandRan
			if code != tt.code {
				t.Errorf("exit code = %d, want %d", code, tt.code)
			}
		})
	}
}
