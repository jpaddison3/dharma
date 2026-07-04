package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/jpaddison3/dharma/internal/client"
	"github.com/jpaddison3/dharma/internal/output"
	"github.com/spf13/cobra"
)

// paginateHintFor returns the list hint shown when a next page exists but
// --paginate wasn't passed, or "" otherwise. One home for both the string and
// the hasMore→hint derivation so the call sites (runList, task stories) can't drift.
func paginateHintFor(hasMore bool) string {
	if hasMore {
		return "more pages exist — rerun with --paginate to fetch all"
	}
	return ""
}

// fieldsFlagUsage is the one usage string for every curated --fields flag, so
// the empty-string escape hatch is documented identically everywhere.
const fieldsFlagUsage = "opt_fields (curated default; pass --fields \"\" for Asana's raw fields)"

// addFieldsFlag registers the standard --fields flag (curated default plus the
// shared usage string) on a list/get command.
func addFieldsFlag(cmd *cobra.Command, dest *string, defaultVal string) {
	cmd.Flags().StringVar(dest, "fields", defaultVal, fieldsFlagUsage)
}

// setOptFields sets opt_fields on q unless fields is empty, in which case Asana
// returns its raw default representation. One home for the empty-means-raw contract.
func setOptFields(q url.Values, fields string) {
	if fields != "" {
		q.Set("opt_fields", fields)
	}
}

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
	return output.PrintList(os.Stdout, all, hasMore, paginateHintFor(hasMore))
}
