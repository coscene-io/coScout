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
