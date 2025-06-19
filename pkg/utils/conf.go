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
