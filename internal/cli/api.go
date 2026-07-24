package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/jpaddison3/dharma/internal/client"
	"github.com/jpaddison3/dharma/internal/output"
	"github.com/spf13/cobra"
)

var (
	apiMethod   string
	apiFields   []string
	apiRawBody  string
	apiPaginate bool
)

var apiCmd = &cobra.Command{
	Use:   "api <path>",
	Short: "Make a raw request to the Asana API",
	Long: `Make a raw request to the Asana API. Path is the part after /api/1.0.

Examples:
  dharma api /users/me
  dharma api -X POST /tasks -f name=Foo -f projects=1234567890
  dharma api /workspaces/123/tasks --paginate
  dharma api -X PUT /tasks/123 --body '{"data": {"completed": true}}'

-f key=value becomes a query parameter on GET/DELETE/HEAD and a body field
(wrapped in Asana's {"data": ...} envelope) on POST/PUT/PATCH. --body passes
raw JSON through unchanged.

Shell-unsafe text (quotes, apostrophes, newlines) can be piped or read from a
file instead of quoted on the command line, for POST/PUT/PATCH only: --body -
and -f key=@- read the value from stdin; --body @file and -f key=@file read it
from a file. At most one '-'/'@-' source is allowed per invocation, since
stdin can only be read once. This only applies to bodies — a GET/DELETE/HEAD
-f value is always literal, so a leading '@' in a query filter (e.g.
text=@handle) passes through unchanged. A body field that must literally
start with '@' can't go through -f; use --body instead.

For arbitrary text, a file needs no shell escaping and can't collide with a
heredoc delimiter:

dharma api -X POST /tasks/123/stories -f text=@- < body.txt

A quoted-delimiter heredoc also works, but its closing delimiter must start
its own line (column 0) and must not appear in the text:

dharma api -X POST /tasks/123/stories -f text=@- <<'DHARMA_EOF'
It's "quoted" text — no escaping needed.
DHARMA_EOF`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		path := args[0]
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}

		method := strings.ToUpper(apiMethod)
		hasBody := method != "GET" && method != "DELETE" && method != "HEAD"

		if apiPaginate && method != "GET" {
			return usageErrorf("--paginate is only supported for GET requests")
		}

		var body interface{}
		var rawBody []byte
		query := url.Values{}

		switch {
		case apiRawBody != "":
			if !hasBody {
				return usageErrorf("--body is not valid for %s requests", method)
			}
			resolvedBody, err := resolveBody(apiRawBody)
			if err != nil {
				return err
			}
			var v interface{}
			if err := json.Unmarshal([]byte(resolvedBody), &v); err != nil {
				return usageErrorf("invalid --body JSON: %v", err)
			}
			rawBody = []byte(resolvedBody)
		case len(apiFields) > 0:
			m, q, err := buildAPIFields(apiFields, hasBody)
			if err != nil {
				return err
			}
			if hasBody {
				body = m
			} else {
				query = q
			}
		}

		ctx := context.Background()

		send := func() (*client.Response, error) {
			if rawBody != nil {
				return c.DoRaw(ctx, method, path, query, rawBody)
			}
			return c.Do(ctx, method, path, query, body)
		}

		if !apiPaginate {
			resp, err := send()
			if err != nil {
				return err
			}
			out := map[string]interface{}{"data": json.RawMessage(resp.Data)}
			if resp.NextPage != nil {
				out["next_page"] = resp.NextPage
			}
			// Always JSON: `dharma api` is the raw, jq-safe escape hatch and must
			// not be TOON-encoded even under --output toon.
			return output.PrintJSON(os.Stdout, out)
		}

		var all []json.RawMessage
		for {
			resp, err := send()
			if err != nil {
				return err
			}
			var chunk []json.RawMessage
			if err := json.Unmarshal(resp.Data, &chunk); err != nil {
				return fmt.Errorf("--paginate expects an array response: %w", err)
			}
			all = append(all, chunk...)
			if resp.NextPage == nil || resp.NextPage.Offset == "" {
				break
			}
			query.Set("offset", resp.NextPage.Offset)
		}
		return output.PrintJSON(os.Stdout, map[string]interface{}{"data": all})
	},
}

// resolveBody resolves a --body value: a lone "-" or "@-" reads stdin, "@file"
// reads a file, anything else is a literal. It shares the @-grammar with -f via
// expandAtValue (bare "-" is a --body-only alias for "@-"), so the two input
// paths can't drift and the file/stdin branches are covered by expandAtValue's
// tests. Trailing-newline trimming is inert here — the caller JSON-parses the
// result, and JSON ignores trailing whitespace.
func resolveBody(spec string) (string, error) {
	if spec == "-" {
		spec = "@-"
	}
	return expandAtValue(spec)
}

// buildAPIFields splits each key=value -f entry. On body methods a leading '@'
// is expanded (file/stdin) into a JSON body map; otherwise values stay literal
// query parameters. Both directions live here so a test can assert the
// security-relevant invariant: '@' expands only when there is a request body,
// never in a GET/DELETE/HEAD query value.
func buildAPIFields(fields []string, hasBody bool) (map[string]string, url.Values, error) {
	if hasBody {
		m := make(map[string]string)
		for _, f := range fields {
			k, v, ok := strings.Cut(f, "=")
			if !ok {
				return nil, nil, usageErrorf("--field must be key=value, got %q", f)
			}
			v, err := expandAtValue(v)
			if err != nil {
				return nil, nil, err
			}
			m[k] = v
		}
		return m, nil, nil
	}
	query := url.Values{}
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			return nil, nil, usageErrorf("--field must be key=value, got %q", f)
		}
		query.Add(k, v)
	}
	return nil, query, nil
}

func init() {
	apiCmd.Flags().StringVarP(&apiMethod, "method", "X", "GET", "HTTP method")
	apiCmd.Flags().StringArrayVarP(&apiFields, "field", "f", nil, "key=value field (repeatable)")
	apiCmd.Flags().StringVar(&apiRawBody, "body", "", "raw JSON body, or '-'/'@file' to read it (overrides --field, no envelope wrapping)")
	apiCmd.Flags().BoolVar(&apiPaginate, "paginate", false, "follow next_page for collection endpoints (GET only)")
}
