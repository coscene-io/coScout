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
