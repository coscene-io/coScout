package mod

import (
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/storage"
)

type CustomHandler interface {
	// Run the mod handler
	Run() error
}

type Handler struct {
	reqClient api.RequestClient
	config    config.AppConfig
	storage   storage.Storage
}
