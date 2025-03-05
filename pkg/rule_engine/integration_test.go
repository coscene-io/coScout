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
	"testing"
	"time"

	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/enums"
	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestApiRuleToRuleSpec(t *testing.T) {
	t.Parallel()

	apiRule := &resources.DiagnosisRule{
		Name:         "test",
		DisplayName:  "test display",
		ActiveTopics: "test topic",
		Each: []*structpb.Struct{
			{
				Fields: map[string]*structpb.Value{
					"key": {
						Kind: &structpb.Value_StringValue{
							StringValue: "value1",
						},
					},
				},
			},
			{
				Fields: map[string]*structpb.Value{
					"key": {
						Kind: &structpb.Value_StringValue{
							StringValue: "value2",
						},
					},
				},
			},
		},
		Version: resources.DiagnosisRule_VERSION_2,
		ConditionSpecs: []*resources.ConditionSpec{
			{
				Condition: &resources.ConditionSpec_Raw{
					Raw: "msg.message.contains(\"error 1\")",
				},
			},
			{
				Condition: &resources.ConditionSpec_Structured{
					Structured: &resources.StructuredConditionSpec{
						Type: enums.RuleConditionTypeEnum_STRING,
						Path: "msg.message",
						Op:   enums.RuleConditionOpEnum_CONTAINS,
						Value: &resources.StructuredConditionSpec_UserInput{
							UserInput: "error 1",
						},
					},
				},
			},
		},
		DebounceDuration: "PT10S",
		ActionSpecs: []*resources.ActionSpec{
			{
				Spec: &resources.ActionSpec_Upload{
					Upload: &resources.UploadActionSpec{
						PreTrigger:  "10s",
						PostTrigger: "10s",
						Title:       "upload title",
						Description: "upload description",
					},
				},
			},
		},
	}
	rules, validationResult := ValidateApiRule(apiRule, map[string]ActionImpl{
		"upload": EmptyActionImpl,
	})
	if !validationResult.Success {
		t.Fatalf("Failed to validate rule spec: %v", validationResult)
	}
	evalResult := rules[0].EvalConditions(map[string]interface{}{
		"msg": map[string]interface{}{
			"message": "error 1",
		},
	}, nil, time.Now())
	if !evalResult {
		t.Errorf("Expected true, got false")
	}
}
