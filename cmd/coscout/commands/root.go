package commands

import (
	"github.com/coscene-io/coscout"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"path"
	"runtime"
	"strconv"
)

func NewCommand() *cobra.Command {
	var (
		cfgPath  = ""
		logLevel = ""
	)

	cmd := &cobra.Command{
		Use: "cos",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, args)
		},
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			level, err := log.ParseLevel(logLevel)
			if err != nil {
				log.Fatal(err)
			}

			log.SetLevel(level)
			log.SetReportCaller(true)
			log.SetFormatter(&log.TextFormatter{
				FullTimestamp: true,
				CallerPrettyfier: func(frame *runtime.Frame) (function string, file string) {
					fileName := path.Base(frame.File)
					return strconv.Itoa(frame.Line), fileName
				},
			})
		},
		Version: coscout.GetVersion(),
	}

	cmd.PersistentFlags().StringVarP(&cfgPath, "config-path", "c", "$HOME/.config/cos/config.yaml", "config path")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "set the logging level, one of: debug|info|warn|error")

	cmd.AddCommand(NewVersionCommand())
	cmd.AddCommand(NewDaemonCommand(&cfgPath))
	return cmd
}
