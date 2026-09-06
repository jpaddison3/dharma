package cli

import (
	"context"
	"encoding/json"
	"os"

	"github.com/jpaddison3/dharma/internal/client"
	"github.com/jpaddison3/dharma/internal/output"
	"github.com/spf13/cobra"
)

var taskDeleteCmd = &cobra.Command{
	Use:   "delete <gid>",
	Short: "Immediately delete a task",
	Long: `Immediately delete one task without a confirmation prompt.

Success prints the task deletion result in dharma's object envelope. Empty
responses produce data:null. Failed requests are not retried automatically;
check the task's state before manually retrying an ambiguous failure.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runTaskDelete(cmd.Context(), c, args[0])
	},
}

func runTaskDelete(ctx context.Context, c *client.Client, gid string) error {
	resp, err := c.Do(ctx, "DELETE", "/tasks/"+gid, nil, nil)
	if err != nil {
		return err
	}
	var value interface{}
	if len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, &value); err != nil {
			return err
		}
	}
	return output.PrintObject(os.Stdout, value)
}

func init() {
	taskCmd.AddCommand(taskDeleteCmd)
}
