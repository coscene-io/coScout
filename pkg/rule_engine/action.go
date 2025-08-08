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
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/pkg/errors"
)

// Action represents an action with a name and parameters.
type Action struct {
	Name      string
	RawKwargs map[string]interface{}
	kwargs    map[string]expressionEvaluator
	impl      ActionImpl
}

// expressionEvaluator is a function that evaluates an expression with given activation.
type expressionEvaluator func(activation map[string]interface{}) (interface{}, error)

// ActionImpl is the alias for the action implementation function type.
type ActionImpl = func(map[string]interface{}) error

func EmptyActionImpl(_ map[string]interface{}) error {
	return nil
}

// NewAction creates a new action with compiled CEL expressions.
func NewAction(
	name string,
	rawKwargs map[string]interface{},
	impl ActionImpl,
) (*Action, error) {
	if impl == nil {
		return nil, errors.New("implementation function cannot be nil")
	}

	env, err := NewEnv()
	if err != nil {
		return nil, errors.Errorf("failed to create CEL environment: %v", err)
	}

	kwargs := make(map[string]expressionEvaluator)

	// Add built-in variables
	rawKwargs["ts"] = "{ts}"

	for k, v := range rawKwargs {
		switch val := v.(type) {
		case string:
			evaluator, err := compileEmbeddedExpr(env, val)
			if err != nil {
				return nil, errors.Errorf("failed to compile argument %s: %v", k, err)
			}
			kwargs[k] = evaluator
		case []string:
			evaluator, err := compileArrayExpr(env, val)
			if err != nil {
				return nil, errors.Errorf("failed to compile array argument %s: %v", k, err)
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

// Run executes the action with the given activation context as well as additional kwargs.
// Note that it's user's responsibility to ensure that the additional kwargs do not conflict
// with fixed kwargs.
func (a *Action) Run(activation map[string]interface{}, kwargs map[string]interface{}) error {
	evaluatedKwargs := make(map[string]interface{})

	for k, evaluator := range a.kwargs {
		value, err := evaluator(activation)
		if err != nil {
			return errors.Errorf("failed to evaluate argument %s: %v", k, err)
		}
		evaluatedKwargs[k] = value
	}

	// Add additional kwargs
	for k, v := range kwargs {
		evaluatedKwargs[k] = v
	}

	return a.impl(evaluatedKwargs)
}

// RunDirect executes the action with the given activation context.
func (a *Action) RunDirect(activation map[string]interface{}) error {
	return a.Run(activation, nil)
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

			var formattedValue string
			switch v := val.Value().(type) {
			case float64:
				formattedValue = strings.TrimRight(strconv.FormatFloat(v, 'f', -1, 64), "0")
			default:
				formattedValue = fmt.Sprintf("%v", v)
			}

			result = strings.Replace(result, matches[i][0], formattedValue, 1)
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

// compileArrayExpr compiles expressions in a string array.
// Each array element can contain embedded CEL expressions like "{scope.files}".
func compileArrayExpr(env *cel.Env, arr []string) (expressionEvaluator, error) {
	compiledEvaluators := make([]expressionEvaluator, len(arr))

	for i, element := range arr {
		pattern := regexp.MustCompile(`\{\s*(.*?)\s*}`)
		matches := pattern.FindAllStringSubmatch(element, -1)

		switch {
		case len(matches) == 0:
			// If no expressions found, return the constant string
			compiledEvaluators[i] = func(map[string]interface{}) (interface{}, error) {
				return element, nil
			}
		case len(matches) == 1 && matches[0][0] == element:
			// Special case: element is pure expression like "{scope.files}"
			// Try to evaluate it as an array expression first
			ast, issues := env.Compile(matches[0][1])
			if issues != nil && issues.Err() != nil {
				return nil, errors.Errorf("failed to compile array expression '%s': %v", matches[0][1], issues.Err())
			}
			program, err := env.Program(ast)
			if err != nil {
				return nil, errors.Errorf("failed to create program for array expression '%s': %v", matches[0][1], err)
			}

			compiledEvaluators[i] = func(activation map[string]interface{}) (interface{}, error) {
				val, _, err := program.Eval(activation)
				if err != nil {
					return nil, errors.Errorf("failed to evaluate array expression '%s': %v", matches[0][1], err)
				}
				return val.Value(), nil
			}
		default:
			// Element contains mixed content with expressions, treat as string
			evaluator, err := compileEmbeddedExpr(env, element)
			if err != nil {
				return nil, errors.Errorf("failed to compile embedded expression in array element: %v", err)
			}
			compiledEvaluators[i] = evaluator
		}
	}

	return func(activation map[string]interface{}) (interface{}, error) {
		result := make([]string, 0)

		for _, evaluator := range compiledEvaluators {
			value, err := evaluator(activation)
			if err != nil {
				return nil, err
			}

			switch v := value.(type) {
			case []interface{}:
				// If the expression evaluates to an array, append all elements
				for _, item := range v {
					result = append(result, fmt.Sprintf("%v", item))
				}
			case []string:
				// If it's already a string array, append directly
				result = append(result, v...)
			case string:
				// Single string value
				if v != "" {
					result = append(result, v)
				}
			default:
				// Convert other types to string
				if str := fmt.Sprintf("%v", v); str != "" {
					result = append(result, str)
				}
			}
		}

		return result, nil
	}, nil
}

func (a *Action) String() string {
	return fmt.Sprintf("Action(%s)%v", a.Name, a.RawKwargs)
}
