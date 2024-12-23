package utils

import (
	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/pkg/errors"
)

func ParseRawJson(rawData []byte, v interface{}) error {
	k := koanf.New(".")
	if err := k.Load(rawbytes.Provider(rawData), json.Parser()); err != nil {
		return errors.Wrapf(err, "load raw json koanf")
	}

	if err := k.Unmarshal("", v); err != nil {
		return errors.Wrapf(err, "unmarshal koanf raw json")
	}
	return nil
}
