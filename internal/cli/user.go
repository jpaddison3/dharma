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
		if userMeFields != "" {
			q.Set("opt_fields", userMeFields)
		}
		return runGet(context.Background(), c, "/users/me", q)
	},
}

func init() {
	userMeCmd.Flags().StringVar(&userMeFields, "fields", userMeDefaultFields, "opt_fields (curated default; pass --fields \"\" for Asana's raw fields)")
	userCmd.AddCommand(userMeCmd)
}
