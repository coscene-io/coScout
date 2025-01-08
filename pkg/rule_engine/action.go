package rule_engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/pkg/errors"
)

// Action represents an action with a name and parameters.
type Action struct {
	Name      string
	RawKwargs map[string]interface{}
	kwargs    map[string]expressionEvaluator
	impl      func(map[string]interface{}) error
}

// expressionEvaluator is a function that evaluates an expression with given activation.
type expressionEvaluator func(activation map[string]interface{}) (interface{}, error)

// NewAction creates a new action with compiled CEL expressions.
func NewAction(
	name string,
	rawKwargs map[string]interface{},
	impl func(map[string]interface{}) error,
) (*Action, error) {
	if impl == nil {
		return nil, errors.New("implementation function cannot be nil")
	}

	env, err := cel.NewEnv(
		cel.Variable("msg", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("scope", cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable("topic", cel.StringType),
		cel.Variable("ts", cel.DoubleType),
	)
	if err != nil {
		return nil, errors.Errorf("failed to create CEL environment: %v", err)
	}

	kwargs := make(map[string]expressionEvaluator)
	for k, v := range rawKwargs {
		switch val := v.(type) {
		case string:
			evaluator, err := compileEmbeddedExpr(env, val)
			if err != nil {
				return nil, errors.Errorf("failed to compile argument %s: %v", k, err)
			}
			kwargs[k] = evaluator
		case map[string]interface{}:
			evaluator, err := compileDictExpr(env, val)
			if err != nil {
				return nil, errors.Errorf("failed to compile dict argument %s: %v", k, err)
			}
			kwargs[k] = evaluator
		default:
			// For non-string values, create a simple evaluator that returns the constant
			kwargs[k] = func(map[string]interface{}) (interface{}, error) {
				return v, nil
			}
		}
	}

	return &Action{
		Name:      name,
		RawKwargs: rawKwargs,
		kwargs:    kwargs,
		impl:      impl,
	}, nil
}

// Run executes the action with the given activation context.
func (a *Action) Run(activation map[string]interface{}) error {
	evaluatedKwargs := make(map[string]interface{})

	for k, evaluator := range a.kwargs {
		value, err := evaluator(activation)
		if err != nil {
			return errors.Errorf("failed to evaluate argument %s: %v", k, err)
		}
		evaluatedKwargs[k] = value
	}

	return a.impl(evaluatedKwargs)
}

// compileEmbeddedExpr compiles a string containing embedded CEL expressions.
func compileEmbeddedExpr(env *cel.Env, expr string) (expressionEvaluator, error) {
	pattern := regexp.MustCompile(`\{\s*(.*?)\s*}`)
	matches := pattern.FindAllStringSubmatch(expr, -1)

	if len(matches) == 0 {
		// If no expressions found, return the constant string
		return func(map[string]interface{}) (interface{}, error) {
			return expr, nil
		}, nil
	}

	programs := make([]cel.Program, 0)
	for _, match := range matches {
		ast, issues := env.Compile(match[1])
		if issues != nil && issues.Err() != nil {
			return nil, errors.Errorf("failed to compile expression '%s': %v", match[1], issues.Err())
		}
		program, err := env.Program(ast)
		if err != nil {
			return nil, errors.Errorf("failed to create program for '%s': %v", match[1], err)
		}
		programs = append(programs, program)
	}

	return func(activation map[string]interface{}) (interface{}, error) {
		result := expr
		for i, program := range programs {
			val, _, err := program.Eval(activation)
			if err != nil {
				// Replace failed expressions with "{ ERROR }"
				result = strings.Replace(result, matches[i][0], "{ ERROR }", 1)
				continue
			}
			result = strings.Replace(result, matches[i][0], fmt.Sprintf("%v", val.Value()), 1)
		}
		return result, nil
	}, nil
}

// compileDictExpr compiles expressions in a dictionary.
func compileDictExpr(env *cel.Env, dict map[string]interface{}) (expressionEvaluator, error) {
	compiledDict := make(map[string]expressionEvaluator)

	for k, v := range dict {
		switch val := v.(type) {
		case string:
			evaluator, err := compileEmbeddedExpr(env, val)
			if err != nil {
				return nil, errors.Errorf("failed to compile dict value for key %s: %v", k, err)
			}
			compiledDict[k] = evaluator
		case map[string]interface{}:
			return nil, errors.Errorf("nested dictionaries are not supported")
		default:
			// For non-string values, create a simple evaluator that returns the constant
			compiledDict[k] = func(map[string]interface{}) (interface{}, error) {
				return v, nil
			}
		}
	}

	return func(activation map[string]interface{}) (interface{}, error) {
		result := make(map[string]interface{})
		for k, evaluator := range compiledDict {
			value, err := evaluator(activation)
			if err != nil {
				return nil, errors.Errorf("failed to evaluate dict value for key %s: %v", k, err)
			}
			result[k] = value
		}
		return result, nil
	}, nil
}

func (a *Action) String() string {
	return fmt.Sprintf("Action(%s)%v", a.Name, a.RawKwargs)
}
