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
raw JSON through unchanged.`,
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
			var v interface{}
			if err := json.Unmarshal([]byte(apiRawBody), &v); err != nil {
				return usageErrorf("invalid --body JSON: %v", err)
			}
			rawBody = []byte(apiRawBody)
		case len(apiFields) > 0:
			if hasBody {
				m := make(map[string]string)
				for _, f := range apiFields {
					k, v, ok := strings.Cut(f, "=")
					if !ok {
						return usageErrorf("--field must be key=value, got %q", f)
					}
					m[k] = v
				}
				body = m
			} else {
				for _, f := range apiFields {
					k, v, ok := strings.Cut(f, "=")
					if !ok {
						return usageErrorf("--field must be key=value, got %q", f)
					}
					query.Add(k, v)
				}
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
			return output.Print(os.Stdout, out)
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
		return output.Print(os.Stdout, map[string]interface{}{"data": all})
	},
}

func init() {
	apiCmd.Flags().StringVarP(&apiMethod, "method", "X", "GET", "HTTP method")
	apiCmd.Flags().StringArrayVarP(&apiFields, "field", "f", nil, "key=value field (repeatable)")
	apiCmd.Flags().StringVar(&apiRawBody, "body", "", "raw JSON body (overrides --field, no envelope wrapping)")
	apiCmd.Flags().BoolVar(&apiPaginate, "paginate", false, "follow next_page for collection endpoints (GET only)")
}
