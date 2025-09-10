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

type AppConfig struct {
	Api ApiConfig `koanf:"api" yaml:"api" json:"api"`

	// Collector CollectorConfig `yaml:"collector"`
	Collector CollectorConfig `koanf:"collector" yaml:"collector" json:"collector"`

	// Device DeviceConfig `yaml:"device"`
	Device DeviceConfig `koanf:"device" yaml:"device" json:"device"`

	// Topics []string `yaml:"topics"`
	Topics []string `koanf:"topics" yaml:"topics" json:"topics"`

	// Register RegisterConfig `yaml:"register"`
	Register RegisterConfig `koanf:"register" yaml:"register" json:"register"`

	// Mod config
	Mod ModConfConfig `koanf:"mod" yaml:"mod" json:"mod"`

	// HttpServer HttpServerConfig `yaml:"http_server"`
	HttpServer HttpServerConfig `koanf:"http_server" yaml:"http_server" json:"http_server"`

	// PluginConfig other coscene plugin config, such as coEncoder..., which is not cos's config
	PluginConfig interface{} `koanf:"plugin_config" yaml:"plugin_config" json:"plugin_config"`

	// Upload
	Upload UploadConfig `koanf:"upload" yaml:"upload" json:"upload"`

	// Master-Slave configuration
	MasterSlave MasterSlaveConfig `koanf:"master_slave" yaml:"master_slave" json:"master_slave"`

	// import other config
	Import []string `koanf:"__import__" yaml:"__import__" json:"__import__"`
}

type ApiConfig struct {
	ServerURL   string `koanf:"server_url"   yaml:"server_url" json:"server_url"`
	ProjectSlug string `koanf:"project_slug" yaml:"project_slug" json:"project_slug"`
	OrgSlug     string `koanf:"org_slug"     yaml:"org_slug" json:"org_slug"`
	Insecure    bool   `koanf:"insecure"     yaml:"insecure" json:"insecure"`
}

type CollectorConfig struct {
	DeleteAfterIntervalInHours int  `koanf:"delete_after_interval_in_hours" yaml:"delete_after_interval_in_hours" json:"delete_after_interval_in_hours"`
	SkipCheckSameFile          bool `koanf:"skip_check_same_file" yaml:"skip_check_same_file" json:"skip_check_same_file"`
}

type DeviceConfig struct {
	ExtraFiles []string `koanf:"extra_files" yaml:"extra_files" json:"extra_files"`
}

type RegisterConfig struct {
	Provider string      `koanf:"type"   yaml:"type" json:"type"`
	Conf     interface{} `koanf:"config" yaml:"config" json:"config"`
}

type FileModRegisterConfig struct {
	SnFile  string `koanf:"sn_file"  yaml:"sn_file" json:"sn_file"`
	SnField string `koanf:"sn_field" yaml:"sn_field" json:"sn_field"`
}

type ModConfConfig struct {
	Name   string      `koanf:"name" yaml:"name" json:"name"`
	Config interface{} `koanf:"conf" yaml:"conf" json:"conf"`
}

type DefaultModConfConfig struct {
	ListenDirs            []string `koanf:"listen_dirs" yaml:"listen_dirs" json:"listen_dirs"`
	CollectDirs           []string `koanf:"collect_dirs" yaml:"collect_dirs" json:"collect_dirs"`
	Ros2CustomizedMsgDirs []string `koanf:"ros2_customized_msgs_dirs" yaml:"ros2_customized_msgs_dirs" json:"ros2_customized_msgs_dirs"`
	UploadFiles           []string `koanf:"upload_files" yaml:"upload_files" json:"upload_files"`
	SkipPeriodHours       int      `koanf:"skip_period_hours" yaml:"skip_period_hours" json:"skip_period_hours"`
	RecursivelyWalkDirs   bool     `koanf:"recursively_walk_dirs" yaml:"recursively_walk_dirs" json:"recursively_walk_dirs"`
}

type HttpServerConfig struct {
	Port int `koanf:"port" yaml:"port" json:"port"`
}

type UploadConfig struct {
	NetworkRule NetworkRule `koanf:"network_rule" yaml:"network_rule" json:"network_rule"`
}

type NetworkRule struct {
	Enabled         bool     `koanf:"enabled" yaml:"enabled" json:"enabled"`
	BlackInterfaces []string `koanf:"black_interfaces" yaml:"black_interfaces" json:"black_interfaces"`
	Server          string   `koanf:"server" yaml:"server" json:"server"`
}

type MasterSlaveConfig struct {
	// Whether to enable master mode (default false)
	Enabled bool `koanf:"enabled" yaml:"enabled" json:"enabled"`
}
