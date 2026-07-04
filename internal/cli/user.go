package cli

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "User commands",
}

var userMeCmd = &cobra.Command{
	Use:   "me",
	Short: "Show the authenticated user",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runGet(context.Background(), c, "/users/me", nil)
	},
}

var (
	userListName     string
	userListFields   string
	userListPaginate bool
)

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List users in a workspace; --name uses Asana's typeahead for fuzzy matching",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx := context.Background()
		ws, err := requireWorkspace(ctx, c)
		if err != nil {
			return err
		}
		if userListName != "" {
			q := url.Values{"resource_type": []string{"user"}, "query": []string{userListName}}
			if userListFields != "" {
				q.Set("opt_fields", userListFields)
			}
			return runList(ctx, c, "/workspaces/"+ws+"/typeahead", q, false)
		}
		q := url.Values{"workspace": []string{ws}}
		if userListFields != "" {
			q.Set("opt_fields", userListFields)
		}
		return runList(ctx, c, "/users", q, userListPaginate)
	},
}

var userGetFields string

var userGetCmd = &cobra.Command{
	Use:   "get <gid>",
	Short: "Fetch a user (gid, 'me', or email address)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		q := url.Values{}
		if userGetFields != "" {
			q.Set("opt_fields", userGetFields)
		}
		return runGet(context.Background(), c, "/users/"+args[0], q)
	},
}

func init() {
	userListCmd.Flags().StringVar(&userListName, "name", "", "fuzzy match against user names (uses typeahead; max ~20 results)")
	userListCmd.Flags().StringVar(&userListFields, "fields", "name,email", "opt_fields, e.g. name,email")
	userListCmd.Flags().BoolVar(&userListPaginate, "paginate", false, "fetch all pages (ignored when --name is set)")

	userGetCmd.Flags().StringVar(&userGetFields, "fields", "name,email", "opt_fields, e.g. name,email")

	userCmd.AddCommand(userMeCmd, userListCmd, userGetCmd)
}
