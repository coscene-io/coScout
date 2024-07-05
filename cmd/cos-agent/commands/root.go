package commands

import (
	cosagent "github.com/coscene-io/cos-agent"
	"github.com/coscene-io/x/log"
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	var (
		cfgPath  = ""
		logLevel = ""
		logJSON  = false
	)

	cmd := &cobra.Command{
		Use: "cos",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, args)
		},
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			log.SetLevelFormatter(logLevel, logJSON)
		},
		Version: cosagent.GetVersion(),
	}

	cmd.PersistentFlags().StringVarP(&cfgPath, "config-path", "c", "$HOME/.config/cos/config.yaml", "config path")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "set the logging level, one of: debug|info|warn|error")
	cmd.PersistentFlags().BoolVar(&logJSON, "log-json", false, "set the json logging format")

	cmd.AddCommand(NewVersionCommand())
	cmd.AddCommand(NewDaemonCommand(&cfgPath))
	return cmd
}
