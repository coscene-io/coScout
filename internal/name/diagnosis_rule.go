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
	"fmt"

	"github.com/oriser/regroup"
	"github.com/pkg/errors"
)

type DiagnosisRule struct {
	ProjectId          string
	DiagnosisRuleSetId string
	Id                 string
}

var (
	//nolint: gochecknoglobals // This is a constant.
	diagnosisRuleNameRe = regroup.MustCompile(`^projects/(?P<project>.*)/diagnosisRuleSets/(?P<drs>.*)/diagnosisRules/(?P<dr>.*)$`)
)

func NewDiagnosisRule(dr string) (*DiagnosisRule, error) {
	if match, err := diagnosisRuleNameRe.Groups(dr); err != nil {
		return nil, errors.Wrap(err, "parse diagnosis rule name")
	} else {
		return &DiagnosisRule{ProjectId: match["project"], DiagnosisRuleSetId: match["drs"], Id: match["dr"]}, nil
	}
}

func (dr DiagnosisRule) Project() Project {
	return Project{Id: dr.ProjectId}
}

func (dr DiagnosisRule) String() string {
	return fmt.Sprintf("projects/%s/diagnosisRuleSets/%s/diagnosisRules/%s", dr.ProjectId, dr.DiagnosisRuleSetId, dr.Id)
}
