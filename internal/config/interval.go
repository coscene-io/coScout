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

package config

import "time"

const (
	// CollectionInterval is the interval at which data is collected to upload.
	CollectionInterval = 60 * time.Second

	// HeartbeatInterval is the interval at which the heartbeat is sent.
	HeartbeatInterval = 60 * time.Second

	// RefreshRemoteConfigInterval is the interval at which the remote config is refreshed.
	RefreshRemoteConfigInterval = 60 * time.Second

	// TaskCheckInterval is the interval at which the task is checked.
	TaskCheckInterval = 60 * time.Second

	// ReloadRulesInterval is the interval at which the rules are reloaded.
	ReloadRulesInterval = 60 * time.Second

	// RuleCheckListenFilesInterval is the periodic interval to listen for files to be processed.
	RuleCheckListenFilesInterval = 23 * time.Second

	// RuleScanCollectInfosInterval is the interval at which the rule is scanned to collect infos.
	RuleScanCollectInfosInterval = 27 * time.Second

	// DeviceAuthCheckInterval is the interval at which the device is checked for authorization.
	DeviceAuthCheckInterval = 60 * time.Second
)
