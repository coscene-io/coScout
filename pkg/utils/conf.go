package utils

import (
	"github.com/knadh/koanf"
	yamlparser "github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/pkg/errors"
)

// ParseYAML Parse load koanf config from specified local path into v.
func ParseYAML(path string, v interface{}) error {
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yamlparser.Parser()); err != nil {
		return errors.Wrapf(err, "load yaml koanf")
	}

	if err := k.Unmarshal("", v); err != nil {
		return errors.Wrapf(err, "unmarshal koanf yaml")
	}
	return nil
}
