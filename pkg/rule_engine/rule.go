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

	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	mapset "github.com/deckarep/golang-set/v2"
)

func AllowedVersions() []string {
	return []string{resources.DiagnosisRule_VERSION_2.String()}
}

// Rule represents a rule with conditions and actions.
type Rule struct {
	Conditions         []Condition
	Actions            []Action
	Scope              map[string]string
	Topics             mapset.Set[string]
	DebounceTime       time.Duration
	prevActivationTime *time.Time
	Metadata           map[string]interface{}
}

// NewRule creates a new Rule instance.
func NewRule(
	conditions []Condition,
	actions []Action,
	scope map[string]string,
	topics mapset.Set[string],
	debounceTime time.Duration,
	metadata map[string]interface{},
) *Rule {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	return &Rule{
		Conditions:   conditions,
		Actions:      actions,
		Scope:        scope,
		Topics:       topics,
		DebounceTime: debounceTime,
		Metadata:     metadata,
	}
}

// EvalConditions evaluates all conditions of the rule.
func (r *Rule) EvalConditions(activation map[string]interface{}, ts time.Time) bool {
	// Check all conditions
	for _, cond := range r.Conditions {
		if !cond.Evaluate(activation) {
			return false
		}
	}

	// If no debounce time set, return true
	if r.DebounceTime <= 0 {
		return true
	}

	// Handle debouncing
	var activated bool
	switch {
	case r.prevActivationTime == nil:
		activated = true
	case ts.Sub(*r.prevActivationTime) > r.DebounceTime:
		activated = true
	default:
		activated = false
	}

	r.prevActivationTime = &ts
	return activated
}
