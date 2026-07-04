package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/jpaddison3/dharma/internal/client"
	"github.com/jpaddison3/dharma/internal/output"
)

func runGet(ctx context.Context, c *client.Client, path string, q url.Values) error {
	var v interface{}
	if err := c.Get(ctx, path, q, &v); err != nil {
		return err
	}
	return output.PrintObject(os.Stdout, v)
}

func runPost(ctx context.Context, c *client.Client, path string, body interface{}) error {
	var v interface{}
	if err := c.Post(ctx, path, body, &v); err != nil {
		return err
	}
	return output.PrintObject(os.Stdout, v)
}

func runPut(ctx context.Context, c *client.Client, path string, body interface{}) error {
	var v interface{}
	if err := c.Put(ctx, path, body, &v); err != nil {
		return err
	}
	return output.PrintObject(os.Stdout, v)
}

// fetchList collects an array endpoint's items, following pagination only when
// paginate is set. hasMore reports that a next page existed but wasn't fetched.
func fetchList(ctx context.Context, c *client.Client, path string, q url.Values, paginate bool) (items []interface{}, hasMore bool, err error) {
	if q == nil {
		q = url.Values{}
	}
	if q.Get("limit") == "" {
		q.Set("limit", "100")
	}
	all := []interface{}{}
	for {
		resp, err := c.Do(ctx, "GET", path, q, nil)
		if err != nil {
			return nil, false, err
		}
		var chunk []interface{}
		if err := json.Unmarshal(resp.Data, &chunk); err != nil {
			return nil, false, fmt.Errorf("list response was not an array: %w", err)
		}
		all = append(all, chunk...)
		if resp.NextPage == nil || resp.NextPage.Offset == "" {
			return all, false, nil
		}
		if !paginate {
			return all, true, nil
		}
		q.Set("offset", resp.NextPage.Offset)
	}
}

func runList(ctx context.Context, c *client.Client, path string, q url.Values, paginate bool) error {
	all, hasMore, err := fetchList(ctx, c, path, q, paginate)
	if err != nil {
		return err
	}
	hint := ""
	if hasMore {
		hint = "more pages exist — rerun with --paginate to fetch all"
	}
	return output.PrintList(os.Stdout, all, hasMore, hint)
}
