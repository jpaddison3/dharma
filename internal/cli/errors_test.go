package cli

import (
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
