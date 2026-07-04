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

func init() {
	addFieldsFlag(userMeCmd, &userMeFields, userMeDefaultFields)
	userCmd.AddCommand(userMeCmd)
}
