package commands

import (
	"os"
	"os/signal"
	"os/user"
	"path"
	"path/filepath"
	"syscall"

	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/register"
	"github.com/coscene-io/coscout/internal/storage"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func NewDaemonCommand(cfgPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run coScout as a daemon",
		Run: func(cmd *cobra.Command, args []string) {
			storageDB := storage.NewBoltDB(getDBPath())
			confManager := config.InitConfManager(*cfgPath, &storageDB)

			appConf := confManager.LoadOnce()
			log.Infof("Load config file from %s", *cfgPath)

			reqClient := api.NewRequestClient(appConf.Api, storageDB)

			registerChan := make(chan register.DeviceStatusResponse)
			reg := register.NewRegister(*reqClient, *appConf, storageDB)
			go reg.CheckOrRegisterDevice(registerChan)

			shutdownChan := make(chan os.Signal, 1)
			signal.Notify(shutdownChan, syscall.SIGINT, syscall.SIGTERM)
			for {
				select {
				case deviceStatus := <-registerChan:
					if deviceStatus.Authorized {
						log.Info("Device is authorized. Performing actions...")
						// Perform actions for authorized device
					} else {
						log.Warn("Device is not authorized.")
						// Handle unauthorized device scenario
					}
				case <-shutdownChan:
					log.Info("Daemon shutdown initiated, stopping...")
					return
				}
			}
		},
	}

	return cmd
}

func getUserBaseFolder() string {
	baseRelativePath := ".local/state/cos"

	u, err := user.Current()
	if err != nil {
		log.Errorf("Get current user failed: %v", err)
		return path.Join(".", baseRelativePath)
	}
	homeDir := u.HomeDir
	return path.Join(homeDir, baseRelativePath)
}

func getDBPath() string {
	dbRelativePath := "db/cos.db"

	dbPath := path.Join(getUserBaseFolder(), dbRelativePath)
	dir := filepath.Dir(dbPath)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			log.Panicf("Create db directory failed: %v", err)
		}
	}

	// Check if the file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		// Create the file if it does not exist
		file, err := os.Create(dbPath)
		if err != nil {
			log.Panicf("Create db file failed: %v", err)
		}
		err = file.Close()
		if err != nil {
			log.Panicf("Close db file failed: %v", err)
		}
	}
	return dbPath
}
