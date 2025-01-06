// Copyright 2025 coScene
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
	//nolint: unused // This is a placeholder for future use
	reqClient api.RequestClient
	//nolint: unused // This is a placeholder for future use
	config config.AppConfig
	//nolint: unused // This is a placeholder for future use
	storage storage.Storage
}
