package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const DefaultBaseURL = "https://app.asana.com/api/1.0"

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	Verbose    bool
}

func New(token string) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		Token:      token,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type APIError struct {
	StatusCode int           `json:"-"`
	Errors     []ErrorDetail `json:"errors"`
	RawBody    string        `json:"-"`
}

type ErrorDetail struct {
	Message string `json:"message"`
	Help    string `json:"help,omitempty"`
}

func (e *APIError) Error() string {
	if msg := e.Message(); msg != "" {
		return fmt.Sprintf("asana api: HTTP %d: %s", e.StatusCode, msg)
	}
	return fmt.Sprintf("asana api: HTTP %d", e.StatusCode)
}

// IsAuth reports whether this is an authentication failure (HTTP 401).
func (e *APIError) IsAuth() bool { return e.StatusCode == 401 }

// Message returns the cleanest human message Asana provided, without the
// "asana api: HTTP N:" prefix that Error() adds. Empty only if Asana gave
// neither a structured error nor a body.
func (e *APIError) Message() string {
	if len(e.Errors) > 0 {
		return e.Errors[0].Message
	}
	if e.RawBody != "" {
		body := e.RawBody
		if len(body) > 200 {
			body = body[:200] + "..."
		}
		return body
	}
	return ""
}

// HelpText returns Asana's actionable help string for this error, if any.
func (e *APIError) HelpText() string {
	if len(e.Errors) > 0 {
		return e.Errors[0].Help
	}
	return ""
}

type Response struct {
	Data     json.RawMessage `json:"data"`
	NextPage *NextPage       `json:"next_page,omitempty"`
}

type NextPage struct {
	Offset string `json:"offset"`
	Path   string `json:"path"`
	URI    string `json:"uri"`
}

// Do wraps body in Asana's `{"data": ...}` envelope before sending.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body interface{}) (*Response, error) {
	var rawBody []byte
	if body != nil {
		b, err := json.Marshal(map[string]interface{}{"data": body})
		if err != nil {
			return nil, fmt.Errorf("encoding body: %w", err)
		}
		rawBody = b
	}
	return c.do(ctx, method, path, query, rawBody)
}

// DoRaw sends rawBody verbatim — caller is responsible for any envelope wrapping.
func (c *Client) DoRaw(ctx context.Context, method, path string, query url.Values, rawBody []byte) (*Response, error) {
	return c.do(ctx, method, path, query, rawBody)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, rawBody []byte) (*Response, error) {
	u, err := url.Parse(c.BaseURL + path)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", c.BaseURL+path, err)
	}
	if len(query) > 0 {
		existing := u.Query()
		for k, vals := range query {
			for _, v := range vals {
				existing.Add(k, v)
			}
		}
		u.RawQuery = existing.Encode()
	}

	var bodyReader io.Reader
	if rawBody != nil {
		bodyReader = bytes.NewReader(rawBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.Verbose {
		fmt.Fprintf(os.Stderr, "> %s %s\n", method, u.String())
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if c.Verbose {
		fmt.Fprintf(os.Stderr, "< %d (%d bytes)\n", resp.StatusCode, len(raw))
	}

	if resp.StatusCode >= 400 {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		if err := json.Unmarshal(raw, apiErr); err != nil || len(apiErr.Errors) == 0 {
			apiErr.RawBody = string(raw)
		}
		return nil, apiErr
	}

	if len(raw) == 0 {
		return &Response{}, nil
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &out, nil
}

func (c *Client) Get(ctx context.Context, path string, query url.Values, out interface{}) error {
	resp, err := c.Do(ctx, "GET", path, query, nil)
	if err != nil {
		return err
	}
	if out == nil || len(resp.Data) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Data, out)
}

func (c *Client) Post(ctx context.Context, path string, body, out interface{}) error {
	resp, err := c.Do(ctx, "POST", path, nil, body)
	if err != nil {
		return err
	}
	if out == nil || len(resp.Data) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Data, out)
}

func (c *Client) Put(ctx context.Context, path string, body, out interface{}) error {
	resp, err := c.Do(ctx, "PUT", path, nil, body)
	if err != nil {
		return err
	}
	if out == nil || len(resp.Data) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Data, out)
}
