package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/jpaddison3/dharma/internal/client"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		commandRan bool
		wantCode   int
		wantStatus int
	}{
		{"usage error from command body", usageErrorf("--name is required"), true, 3, 0},
		{"parse error before any command ran", errors.New("unknown flag: --x"), false, 3, 0},
		{"missing credential", &AuthError{msg: "no token"}, true, 2, 0},
		{"api 401 is auth", &client.APIError{StatusCode: 401}, true, 2, 401},
		{"api 403 is operational", &client.APIError{StatusCode: 403}, true, 1, 403},
		{"wrapped api 401 still auth", fmt.Errorf("ctx: %w", &client.APIError{StatusCode: 401}), true, 2, 401},
		{"plain error after command ran", errors.New("boom"), true, 1, 0},
	}
	// classifyError reads the package-global commandRan; restore it so these
	// subtests can't leak state into any other test in package cli.
	orig := commandRan
	t.Cleanup(func() { commandRan = orig })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandRan = tt.commandRan
			code, payload := classifyError(tt.err)
			if code != tt.wantCode {
				t.Errorf("code = %d, want %d", code, tt.wantCode)
			}
			if payload.HTTPStatus != tt.wantStatus {
				t.Errorf("http_status = %d, want %d", payload.HTTPStatus, tt.wantStatus)
			}
			if payload.Message == "" {
				t.Error("message should never be empty")
			}
		})
	}
}

// The error envelope Execute writes to stdout is a cross-component contract: the
// mcpb shim parses {ok:false,error:{message,http_status?,help?}} to surface the
// status/help. Guard the json tags and the omitempty behavior directly.
func TestErrorEnvelopeShape(t *testing.T) {
	b, err := json.Marshal(errorEnvelope{OK: false, Error: errorPayload{
		Message: "Not Authorized", HTTPStatus: 401, Help: "check the token",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["ok"] != false {
		t.Errorf("ok = %v, want false", m["ok"])
	}
	e, ok := m["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error = %#v", m["error"])
	}
	if e["message"] != "Not Authorized" {
		t.Errorf("message = %v", e["message"])
	}
	if e["http_status"] != float64(401) {
		t.Errorf("http_status = %v, want 401", e["http_status"])
	}
	if e["help"] != "check the token" {
		t.Errorf("help = %v", e["help"])
	}
}

// A usage error carries no HTTP status or help; both are omitempty and must not
// serialize as a zero/empty value the shim would misread as present.
func TestErrorEnvelopeOmitsEmptyStatusAndHelp(t *testing.T) {
	b, err := json.Marshal(errorEnvelope{OK: false, Error: errorPayload{Message: "bad flag"}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	e := m["error"].(map[string]interface{})
	if _, present := e["http_status"]; present {
		t.Error("http_status must be omitted when zero")
	}
	if _, present := e["help"]; present {
		t.Error("help must be omitted when empty")
	}
	if e["message"] != "bad flag" {
		t.Errorf("message = %v", e["message"])
	}
}
