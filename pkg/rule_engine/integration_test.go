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
		Version: "v2",
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
	ruleEngineSpec := ApiRuleToRuleSpec(apiRule)
	rules, validationResult := ValidateRuleSpec(ruleEngineSpec, map[string]interface{}{
		"upload": func(map[string]interface{}) error {
			return nil
		},
	})
	if !validationResult.Success {
		t.Fatalf("Failed to validate rule spec: %v", validationResult)
	}
	evalResult := rules[0].EvalConditions(map[string]interface{}{
		"msg": map[string]interface{}{
			"message": "error 1",
		},
	}, time.Now())
	if !evalResult {
		t.Errorf("Expected true, got false")
	}
}
