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

package name

import (
	"github.com/oriser/regroup"
	"github.com/pkg/errors"
)

type Project struct {
	Id string
}

var (
	//nolint: gochecknoglobals // This is a constant.
	projectNameRe = regroup.MustCompile(`^projects/(?P<project>.*)$`)
)

func NewProject(p string) (*Project, error) {
	if match, err := projectNameRe.Groups(p); err != nil {
		return nil, errors.Wrap(err, "parse project name")
	} else {
		return &Project{Id: match["project"]}, nil
	}
}

func (p Project) String() string {
	return "projects/" + p.Id
}
