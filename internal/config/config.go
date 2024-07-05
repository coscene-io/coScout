package config

type AppConfig struct {
	Api ApiConfig `koanf:"api" yaml:"api"`

	// Register RegisterConfig `yaml:"register"`
	Register RegisterConfig `koanf:"register" yaml:"register"`
	// ConfManager ConfManagerConfig `yaml:"conf_manager"`
	ConfManager ConfManagerConfig `koanf:"configManager" yaml:"configManager"`
	// Collector CollectorConfig `yaml:"collector"`
	Collector CollectorConfig `koanf:"collector" yaml:"collector"`
}

type ApiConfig struct {
	ServerURL   string `koanf:"serverUrl"   yaml:"serverUrl"`
	ProjectSlug string `koanf:"projectSlug" yaml:"projectSlug"`
	OrgSlug     string `koanf:"orgSlug"     yaml:"orgSlug"`
}

type RegisterConfig struct {
	Provider string      `koanf:"type"   yaml:"type"`
	Conf     interface{} `koanf:"config" yaml:"config"`
}

type AgiModRegisterConfig struct {
	RobotYaml string `koanf:"robotYaml" yaml:"robotYaml"`
}

type FileModRegisterConfig struct {
	SnFile  string `koanf:"snFile"  yaml:"snFile"`
	SnField string `koanf:"snField" yaml:"snField"`
}

type ConfManagerConfig struct {
	Interval int      `koanf:"reloadInSecs" yaml:"reloadInSecs"`
	Import   []string `koanf:"import"       yaml:"import"`
}

type CollectorConfig struct {
	Mods []CollectorModConfig `koanf:"mods" yaml:"mods"`
}

type CollectorModConfig struct {
	Provider string      `koanf:"type"    yaml:"type"`
	Enabled  bool        `koanf:"enabled" yaml:"enabled"`
	Conf     interface{} `koanf:"config"  yaml:"config"`
}
