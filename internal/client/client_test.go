package client

import (
	"strings"
	"testing"
)

func TestAPIErrorMessage(t *testing.T) {
	structured := &APIError{
		StatusCode: 403,
		Errors:     []ErrorDetail{{Message: "Forbidden", Help: "check permissions"}},
	}
	if got := structured.Message(); got != "Forbidden" {
		t.Errorf("Message() = %q, want %q", got, "Forbidden")
	}
	if got := structured.HelpText(); got != "check permissions" {
		t.Errorf("HelpText() = %q, want %q", got, "check permissions")
	}
	if got := structured.Error(); got != "asana api: HTTP 403: Forbidden" {
		t.Errorf("Error() = %q", got)
	}

	raw := &APIError{StatusCode: 500, RawBody: "gateway boom"}
	if got := raw.Message(); got != "gateway boom" {
		t.Errorf("Message() = %q, want %q", got, "gateway boom")
	}

	long := &APIError{StatusCode: 500, RawBody: strings.Repeat("x", 300)}
	if got := long.Message(); len(got) != 203 || !strings.HasSuffix(got, "...") {
		t.Errorf("Message() len = %d, want 203 ending in ...", len(got))
	}

	empty := &APIError{StatusCode: 500}
	if got := empty.Message(); got != "" {
		t.Errorf("Message() = %q, want empty", got)
	}
	if got := empty.Error(); got != "asana api: HTTP 500" {
		t.Errorf("Error() = %q, want %q", got, "asana api: HTTP 500")
	}
}

func TestAPIErrorIsAuth(t *testing.T) {
	if !(&APIError{StatusCode: 401}).IsAuth() {
		t.Error("401 should be auth")
	}
	if (&APIError{StatusCode: 403}).IsAuth() {
		t.Error("403 should not be auth")
	}
}
