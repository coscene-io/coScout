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

package rule_engine

import (
	"github.com/google/cel-go/cel"
	"github.com/pkg/errors"
)

// Condition represents a CEL condition.
type Condition struct {
	Raw     string
	program cel.Program
	env     *cel.Env
}

// NewCondition creates and validates a new condition.
func NewCondition(expr string) (*Condition, error) {
	env, err := NewEnv()
	if err != nil {
		return nil, errors.Errorf("failed to create CEL environment: %v", err)
	}

	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, errors.Errorf("failed to compile expression: %v", issues.Err())
	}

	program, err := env.Program(ast)
	if err != nil {
		return nil, errors.Errorf("failed to create program: %v", err)
	}

	return &Condition{
		Raw:     expr,
		program: program,
		env:     env,
	}, nil
}

// Evaluate evaluates the condition against the given activation.
func (c *Condition) Evaluate(activation map[string]interface{}) bool {
	result, _, err := c.program.Eval(activation)
	if err != nil {
		return false
	}

	boolResult, ok := result.Value().(bool)
	if !ok {
		return false
	}

	return boolResult
}
