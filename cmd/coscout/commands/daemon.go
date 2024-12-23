package commands

import (
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/daemon"
	"github.com/coscene-io/coscout/internal/register"
	"github.com/coscene-io/coscout/internal/storage"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"os"
	"os/signal"
	"os/user"
	"path"
	"path/filepath"
	"syscall"
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

			registerChan := make(chan register.DeviceStatusResponse, 1)
			reg := register.NewRegister(*reqClient, appConf, storageDB)
			go reg.CheckOrRegisterDevice(registerChan)

			shutdownChan := make(chan os.Signal, 1)
			signal.Notify(shutdownChan, syscall.SIGINT, syscall.SIGTERM)

			go run(confManager, reqClient, registerChan)

			<-shutdownChan
			log.Info("Daemon shutdown initiated, stopping...")
		},
	}

	return cmd
}

func run(confManager *config.ConfManager, reqClient *api.RequestClient, registerChan chan register.DeviceStatusResponse) {
	startChan := make(chan bool, 1)
	exitChan := make(chan bool, 1)
	errorChan := make(chan error, 100)

	isAuthed := false
	for {
		deviceStatus := <-registerChan
		if deviceStatus.Authorized {
			log.Info("Device is authorized. Performing actions...")

			if !isAuthed {
				go daemon.Run(confManager, reqClient, startChan, exitChan, errorChan)
				startChan <- true
			}
			isAuthed = true
		} else {
			log.Warn("Device is not authorized, waiting...")

			if isAuthed {
				exitChan <- true
			}
			isAuthed = false
		}
	}
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
