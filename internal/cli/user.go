package cli

import (
	"context"

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

func init() {
	userCmd.AddCommand(userMeCmd)
}
