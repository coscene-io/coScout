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
	"time"

	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/itchyny/timefmt-go"
)

// ErrorSection represents the section where validation error occurred.
type ErrorSection int

const (
	ConditionSection ErrorSection = iota + 1
	ActionSection
)

// ValidationErrorLocation represents where the validation error occurred.
type ValidationErrorLocation struct {
	Section   ErrorSection `json:"section"`
	ItemIndex int          `json:"itemIndex,omitempty"`
}

// ValidationErrorUnexpectedVersion represents version validation error.
type ValidationErrorUnexpectedVersion struct {
	AllowedVersions []string `json:"allowedVersions"`
}

// ValidationError represents a validation error.
type ValidationError struct {
	Location          *ValidationErrorLocation          `json:"location,omitempty"`
	UnexpectedVersion *ValidationErrorUnexpectedVersion `json:"unexpectedVersion,omitempty"`
	SyntaxError       *struct{}                         `json:"syntaxError,omitempty"`
	EmptySection      *struct{}                         `json:"emptySection,omitempty"`
}

// ValidationResult represents the result of validation.
type ValidationResult struct {
	Success bool              `json:"success"`
	Errors  []ValidationError `json:"errors"`
}

func CompareValidationResult(result1, result2 ValidationResult) bool {
	if result1.Success != result2.Success {
		return false
	}
	if len(result1.Errors) != len(result2.Errors) {
		return false
	}

	// Compare error counts
	for idx := range result1.Errors {
		if !result1.Errors[idx].Equal(result2.Errors[idx]) {
			return false
		}
	}

	return true
}

// Equal Add this helper method to make ValidationError comparable.
func (e ValidationError) Equal(other ValidationError) bool {
	// Compare Location
	if (e.Location == nil) != (other.Location == nil) {
		return false
	}
	if e.Location != nil && !e.Location.Equal(*other.Location) {
		return false
	}

	// Compare UnexpectedVersion
	if (e.UnexpectedVersion == nil) != (other.UnexpectedVersion == nil) {
		return false
	}
	if e.UnexpectedVersion != nil {
		if !sliceEqual(e.UnexpectedVersion.AllowedVersions, other.UnexpectedVersion.AllowedVersions) {
			return false
		}
	}

	// Compare presence of SyntaxError
	if (e.SyntaxError == nil) != (other.SyntaxError == nil) {
		return false
	}

	// Compare presence of EmptySection
	if (e.EmptySection == nil) != (other.EmptySection == nil) {
		return false
	}

	return true
}

// Equal Add this helper method for ValidationErrorLocation.
func (l ValidationErrorLocation) Equal(other ValidationErrorLocation) bool {
	return l.Section == other.Section &&
		l.ItemIndex == other.ItemIndex
}

// Helper function to compare string slices.
func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func NewEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("msg", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("scope", cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable("topic", cel.StringType),
		cel.Variable("ts", cel.DoubleType),
		cel.Lib(customFunctionLib{}),
	)
}

// RuleItem represents an item to be evaluated by the rule engine.
type RuleItem struct {
	Msg     map[string]interface{} `json:"msg"`
	Topic   string                 `json:"topic"`
	Ts      float64                `json:"ts"` // Timestamp in seconds
	MsgType string                 `json:"msgType"`
	Source  string                 `json:"source"` // filepath or topic of the message
}

type customFunctionLib struct{}

func (customFunctionLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("timestamp",
			cel.Overload("timestamp_double",
				[]*cel.Type{cel.DoubleType},
				cel.TimestampType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					timeFloat, ok := val.Value().(float64)
					if !ok {
						return types.NewErr("expected float64, got %v", val.Value())
					}
					sec, nsec := utils.NormalizeFloatTimestamp(timeFloat)
					return types.Timestamp{Time: time.Unix(sec, int64(nsec))}
				}),
			),
		),

		cel.Function("format",
			cel.MemberOverload("timestamp_format_string",
				[]*cel.Type{cel.TimestampType, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(func(target, arg ref.Val) ref.Val {
					ts, ok := target.(types.Timestamp)
					if !ok {
						return types.NewErr("expected timestamp, got %v", target.Value())
					}
					format, ok := arg.Value().(string)
					if !ok {
						return types.NewErr("expected string, got %v", arg.Value())
					}

					return types.String("UTC: " + timefmt.Format(ts.Time.UTC(), format))
				}),
			),

			cel.MemberOverload("timestamp_format_string_int",
				[]*cel.Type{cel.TimestampType, cel.StringType, cel.IntType},
				cel.StringType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					if len(args) != 3 {
						return types.NewErr("format expects 3 arguments")
					}

					ts, ok := args[0].(types.Timestamp)
					if !ok {
						return types.NewErr("expected timestamp, got %v", args[0].Value())
					}
					format, ok := args[1].Value().(string)
					if !ok {
						return types.NewErr("expected string, got %v", args[1].Value())
					}
					offset, ok := args[2].Value().(int64)
					if !ok {
						return types.NewErr("expected int64, got %v", args[2].Value())
					}

					return types.String(timefmt.Format(ts.Time.UTC().Add(time.Duration(offset)*time.Second), format))
				}),
			),

			cel.MemberOverload("timestamp_format_string_string",
				[]*cel.Type{cel.TimestampType, cel.StringType, cel.StringType},
				cel.StringType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					if len(args) != 3 {
						return types.NewErr("format expects 3 arguments")
					}

					ts, ok := args[0].(types.Timestamp)
					if !ok {
						return types.NewErr("expected timestamp, got %v", args[0].Value())
					}
					format, ok := args[1].Value().(string)
					if !ok {
						return types.NewErr("expected string, got %v", args[1].Value())
					}
					tz, ok := args[2].Value().(string)
					if !ok {
						return types.NewErr("expected string, got %v", args[2].Value())
					}

					loc, err := time.LoadLocation(tz)
					if err != nil {
						return types.NewErr("invalid timezone")
					}

					return types.String(timefmt.Format(ts.Time.In(loc), format))
				}),
			),
		),
	}
}

func (customFunctionLib) ProgramOptions() []cel.ProgramOption {
	return nil
}
