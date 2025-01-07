package rule_engine

import (
	"fmt"

	"github.com/google/cel-go/cel"
)

// Condition represents a CEL condition
type Condition struct {
	Raw     string
	program cel.Program
	env     *cel.Env
}

// NewCondition creates and validates a new condition
func NewCondition(expr string) (*Condition, error) {
	env, err := NewEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %v", err)
	}

	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile expression: %v", issues.Err())
	}

	program, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("failed to create program: %v", err)
	}

	return &Condition{
		Raw:     expr,
		program: program,
		env:     env,
	}, nil
}

// Evaluate evaluates the condition against the given activation
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
