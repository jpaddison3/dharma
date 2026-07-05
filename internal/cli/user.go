package cli

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
)

// userMeDefaultFields trims Asana's /users/me response to the useful bits —
// most notably dropping the five profile-photo URLs. Shared with `auth status`.
const userMeDefaultFields = "name,email,workspaces.name"

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "User commands",
}

var userMeFields string

var userMeCmd = &cobra.Command{
	Use:   "me",
	Short: "Show the authenticated user",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		q := url.Values{}
		setOptFields(q, userMeFields)
		return runGet(context.Background(), c, "/users/me", q)
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
			setOptFields(q, userListFields)
			return runList(ctx, c, "/workspaces/"+ws+"/typeahead", q, false)
		}
		q := url.Values{"workspace": []string{ws}}
		setOptFields(q, userListFields)
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
		setOptFields(q, userGetFields)
		return runGet(context.Background(), c, "/users/"+args[0], q)
	},
}

func init() {
	addFieldsFlag(userMeCmd, &userMeFields, userMeDefaultFields)

	userListCmd.Flags().StringVar(&userListName, "name", "", "fuzzy match against user names (uses typeahead; max ~20 results)")
	addFieldsFlag(userListCmd, &userListFields, "name,email")
	userListCmd.Flags().BoolVar(&userListPaginate, "paginate", false, "fetch all pages (ignored when --name is set)")

	addFieldsFlag(userGetCmd, &userGetFields, "name,email")

	userCmd.AddCommand(userMeCmd, userListCmd, userGetCmd)
}
