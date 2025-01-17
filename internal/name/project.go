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
