package main

import (
	"os"

	"github.com/coscene-io/coscout/cmd/coscout/commands"
	log "github.com/sirupsen/logrus"
)

func main() {
	if err := commands.NewCommand().Execute(); err != nil {
		log.Errorf(err.Error())
		os.Exit(1)
	}
}
