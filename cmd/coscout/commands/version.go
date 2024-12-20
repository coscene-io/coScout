package commands

import (
	cosagent "github.com/coscene-io/coscout"
	"github.com/spf13/cobra"
)

func NewVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			v := cosagent.GetVersion()

			cmd.Println(v)
		},
	}

	return cmd
}
