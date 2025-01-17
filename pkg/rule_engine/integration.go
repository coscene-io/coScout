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

	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/enums"
	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"github.com/coscene-io/coscout/pkg/utils"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/samber/lo"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

// ValidateApiRule validates an API rule and returns a list of rules and validation result.
func ValidateApiRule(apiRule *resources.DiagnosisRule, actionImpls map[string]ActionImpl) ([]*Rule, ValidationResult) {
	// First validate version
	if version := apiRule.GetVersion(); !lo.Contains(AllowedVersions(), version) {
		return nil, ValidationResult{
			Success: false,
			Errors: []ValidationError{{
				Location:          nil,
				UnexpectedVersion: &ValidationErrorUnexpectedVersion{AllowedVersions: AllowedVersions()},
			}},
		}
	}

	var errors []ValidationError

	// Validate and convert conditions
	conditions := make([]Condition, 0)
	conditionSpecs := apiRule.GetConditionSpecs()
	if len(conditionSpecs) == 0 {
		errors = append(errors, ValidationError{
			Location: &ValidationErrorLocation{
				Section: ConditionSection,
			},
			EmptySection: &struct{}{},
		})
	}

	for condIdx, conditionSpec := range conditionSpecs {
		var condStr string
		switch conditionSpec.GetCondition().(type) {
		case *resources.ConditionSpec_Raw:
			condStr = conditionSpec.GetRaw()
		case *resources.ConditionSpec_Structured:
			sc := conditionSpec.GetStructured()
			scPath := sc.GetPath()

			scType := ""
			switch sc.GetType() {
			case enums.RuleConditionTypeEnum_STRING:
				scType = "string"
			case enums.RuleConditionTypeEnum_INT:
				scType = "int"
			case enums.RuleConditionTypeEnum_UINT:
				scType = "uint"
			case enums.RuleConditionTypeEnum_DOUBLE:
				scType = "double"
			case enums.RuleConditionTypeEnum_BOOL:
				scType = "bool"
			case enums.RuleConditionTypeEnum_RULE_CONDITION_TYPE_UNSPECIFIED:
				log.Errorf("unspecified condition type: %v, skipping", sc)
				continue
			}

			scOp := ""
			switch sc.GetOp() {
			case enums.RuleConditionOpEnum_CONTAINS:
				scOp = "contains"
			case enums.RuleConditionOpEnum_EQUAL:
				scOp = "=="
			case enums.RuleConditionOpEnum_RULE_CONDITION_OP_UNSPECIFIED:
				log.Errorf("unspecified condition op: %v, skipping", sc)
				continue
			}

			scValue := ""
			switch sc.GetValue().(type) {
			case *resources.StructuredConditionSpec_Predefined:
				scValue = sc.GetPredefined()
			case *resources.StructuredConditionSpec_UserInput:
				scValue = fmt.Sprintf("%q", sc.GetUserInput())
			}

			if scOp == "contains" {
				condStr = fmt.Sprintf("%s(%s).contains(%s(%s))", scType, scPath, scType, scValue)
			} else {
				condStr = fmt.Sprintf("%s(%s) %s %s(%s)", scType, scPath, scOp, scType, scValue)
			}
		}

		condition, err := NewCondition(condStr)
		if err != nil {
			errors = append(errors, ValidationError{
				Location: &ValidationErrorLocation{
					Section:   ConditionSection,
					ItemIndex: condIdx,
				},
				SyntaxError: &struct{}{},
			})
		} else {
			conditions = append(conditions, *condition)
		}
	}

	// Validate and convert actions
	actions := make([]Action, 0)
	actionSpecs := apiRule.GetActionSpecs()
	if len(actionSpecs) == 0 {
		errors = append(errors, ValidationError{
			Location: &ValidationErrorLocation{
				Section: ActionSection,
			},
			EmptySection: &struct{}{},
		})
	}

	for actionIdx, actionSpec := range actionSpecs {
		var actionName string
		var kwargs map[string]interface{}

		switch spec := actionSpec.GetSpec().(type) {
		case *resources.ActionSpec_Upload:
			actionName = "upload"
			kwargs = map[string]interface{}{
				"before":      spec.Upload.GetPreTrigger(),
				"after":       spec.Upload.GetPostTrigger(),
				"title":       spec.Upload.GetTitle(),
				"description": spec.Upload.GetDescription(),
				"labels":      spec.Upload.GetLabels(),
				"extra_files": spec.Upload.GetExtraFiles(),
				"white_list":  spec.Upload.GetWhiteList(),
			}
		case *resources.ActionSpec_CreateMoment:
			actionName = "create_moment"
			kwargs = map[string]interface{}{
				"title":         spec.CreateMoment.GetTitle(),
				"description":   spec.CreateMoment.GetDescription(),
				"create_task":   spec.CreateMoment.GetCreateTask(),
				"assign_to":     spec.CreateMoment.GetAssignee(),
				"custom_fields": spec.CreateMoment.GetCustomFields(),
			}
		}

		actionImpl := actionImpls[actionName]
		action, err := NewAction(actionName, kwargs, actionImpl)
		if err != nil {
			errors = append(errors, ValidationError{
				Location: &ValidationErrorLocation{
					Section:   ActionSection,
					ItemIndex: actionIdx,
				},
				SyntaxError: &struct{}{},
			})
		} else {
			actions = append(actions, *action)
		}
	}

	if len(errors) > 0 {
		return nil, ValidationResult{
			Success: false,
			Errors:  errors,
		}
	}

	// Convert scopes
	scopes := lo.Map(apiRule.GetEach(), func(each *structpb.Struct, _ int) map[string]string {
		f := each.GetFields()
		vs := make(map[string]string, len(f))
		for k, v := range f {
			vs[k] = v.GetStringValue()
		}
		return vs
	})
	if len(scopes) == 0 {
		scopes = append(scopes, make(map[string]string))
	}

	// Get topics
	topics := mapset.NewSet[string]()
	topics.Add(apiRule.GetActiveTopics())

	// Parse debounce duration
	debounceTime, _ := utils.ParseISODuration(apiRule.GetDebounceDuration())

	// Create rules for each scope
	rules := make([]*Rule, 0)
	for _, scope := range scopes {
		rules = append(rules, NewRule(
			conditions,
			actions,
			scope,
			topics,
			debounceTime,
			nil,
		))
	}

	return rules, ValidationResult{
		Success: true,
		Errors:  nil,
	}
}

func ValidateRuleSpecStr(apiRuleStr string) ([]*Rule, ValidationResult) {
	apiRule := &resources.DiagnosisRule{}
	err := protojson.Unmarshal([]byte(apiRuleStr), apiRule)
	if err != nil {
		return nil, ValidationResult{
			Success: false,
			Errors: []ValidationError{{
				Location:    nil,
				SyntaxError: &struct{}{},
			}},
		}
	}
	actionImpls := map[string]ActionImpl{
		"upload":        EmptyActionImpl,
		"create_moment": EmptyActionImpl,
	}
	return ValidateApiRule(apiRule, actionImpls)
}
