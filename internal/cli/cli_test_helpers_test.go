package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jpaddison3/dharma/internal/output"
	"github.com/spf13/cobra"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type cliTestResult struct {
	stdout  string
	err     error
	code    int
	payload errorPayload
}

// runCLI executes through the registered root command while isolating the
// package globals and Cobra flag state used by these tests. Tests in this file
// family deliberately run serially because os.Stdout and http.DefaultTransport
// are process globals.
func runCLI(t *testing.T, token string, transport roundTripFunc, args ...string) cliTestResult {
	t.Helper()

	type globals struct {
		flagToken, flagWorkspace, flagOutput string
		flagVerbose, commandRan              bool
		format                               string
		tasksFields                          string
		tasksPaginate                        bool
		tasksLimit                           int
		getFields                            string
		removeTag                            string
	}
	original := globals{
		flagToken: flagToken, flagWorkspace: flagWorkspace, flagOutput: flagOutput,
		flagVerbose: flagVerbose, commandRan: commandRan, format: output.Format,
		tasksFields: tagTasksFields, tasksPaginate: tagTasksPaginate, tasksLimit: tagTasksLimit,
		getFields: tagGetFields, removeTag: taskRemoveTagTag,
	}

	type commandFlag struct {
		cmd     *cobra.Command
		name    string
		changed bool
	}
	commandFlags := []commandFlag{
		{cmd: rootCmd, name: "token"},
		{cmd: rootCmd, name: "workspace"},
		{cmd: rootCmd, name: "verbose"},
		{cmd: rootCmd, name: "output"},
		{cmd: tagTasksCmd, name: "fields"},
		{cmd: tagTasksCmd, name: "paginate"},
		{cmd: tagTasksCmd, name: "limit"},
		{cmd: tagGetCmd, name: "fields"},
		{cmd: taskRemoveTagCmd, name: "tag"},
	}
	for i := range commandFlags {
		flag := commandFlags[i].cmd.Flag(commandFlags[i].name)
		commandFlags[i].changed = flag.Changed
		flag.Changed = false
	}

	oldStdout := os.Stdout
	oldTransport := http.DefaultTransport
	oldToken, hadToken := os.LookupEnv("ASANA_TOKEN")
	oldConfigHome, hadConfigHome := os.LookupEnv("XDG_CONFIG_HOME")
	defer func() {
		flagToken, flagWorkspace, flagOutput = original.flagToken, original.flagWorkspace, original.flagOutput
		flagVerbose, commandRan = original.flagVerbose, original.commandRan
		output.Format = original.format
		tagTasksFields, tagTasksPaginate, tagTasksLimit = original.tasksFields, original.tasksPaginate, original.tasksLimit
		tagGetFields, taskRemoveTagTag = original.getFields, original.removeTag
		for _, commandFlag := range commandFlags {
			commandFlag.cmd.Flag(commandFlag.name).Changed = commandFlag.changed
		}
		rootCmd.SetArgs(nil)
		os.Stdout = oldStdout
		http.DefaultTransport = oldTransport
		if hadToken {
			_ = os.Setenv("ASANA_TOKEN", oldToken)
		} else {
			_ = os.Unsetenv("ASANA_TOKEN")
		}
		if hadConfigHome {
			_ = os.Setenv("XDG_CONFIG_HOME", oldConfigHome)
		} else {
			_ = os.Unsetenv("XDG_CONFIG_HOME")
		}
	}()

	flagToken, flagWorkspace, flagOutput = "", "", "json"
	flagVerbose, commandRan = false, false
	output.Format = "json"
	tagTasksFields, tagTasksPaginate, tagTasksLimit = defaultTaskListFields, false, 0
	tagGetFields, taskRemoveTagTag = "name,color", ""
	if err := os.Setenv("ASANA_TOKEN", token); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("XDG_CONFIG_HOME", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if transport == nil {
		transport = func(req *http.Request) (*http.Response, error) {
			t.Fatalf("unexpected HTTP request: %s %s", req.Method, req.URL)
			return nil, nil
		}
	}
	http.DefaultTransport = transport

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	rootCmd.SetArgs(args)
	_, runErr := rootCmd.ExecuteC()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	result := cliTestResult{stdout: string(b), err: runErr}
	if runErr != nil {
		result.code, result.payload = classifyError(runErr)
	}
	return result
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func decodeObject(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decoding output %q: %v", raw, err)
	}
	return got
}

func requireJSONEqual(t *testing.T, got interface{}, want interface{}) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func requireQueryEqual(t *testing.T, got, want url.Values) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("query = %#v, want %#v", got, want)
	}
}
