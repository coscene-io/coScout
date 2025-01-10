package rule_engine

import (
	"fmt"

	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/enums"
	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"github.com/samber/lo"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

type V2Action struct {
	Name   string         `json:"name"`
	Kwargs map[string]any `json:"kwargs"`
}
type V2Rule struct {
	Version           string              `json:"version"`
	Conditions        []string            `json:"conditions"`
	Actions           []V2Action          `json:"actions"`
	Scopes            []map[string]string `json:"scopes"`
	Topics            []string            `json:"topics"`
	ConditionDebounce string              `json:"condition_debounce"`
}

func ApiRuleStrToRuleSpec(apiRuleStr string) (map[string]interface{}, error) {
	apiRule := &resources.DiagnosisRule{}
	err := protojson.Unmarshal([]byte(apiRuleStr), apiRule)
	if err != nil {
		return nil, err
	}
	return ApiRuleToRuleSpec(apiRule), nil
}

func ApiRuleToRuleSpec(apiRule *resources.DiagnosisRule) map[string]interface{} {
	result := make(map[string]interface{})

	var conditions []string
	for _, conditionSpec := range apiRule.GetConditionSpecs() {
		switch conditionSpec.GetCondition().(type) {
		case *resources.ConditionSpec_Raw:
			conditions = append(conditions, conditionSpec.GetRaw())
		case *resources.ConditionSpec_Structured:
			sc := conditionSpec.GetStructured()

			scPath := sc.GetPath()

			scType := ""
			//nolint: exhaustive // we only have two types
			switch sc.GetType() {
			case enums.RuleConditionTypeEnum_STRING:
				scType = "string"
			case enums.RuleConditionTypeEnum_INT:
				scType = "int"
			}

			scOp := ""
			//nolint: exhaustive // we only have two ops
			switch sc.GetOp() {
			case enums.RuleConditionOpEnum_CONTAINS:
				scOp = "contains"
			case enums.RuleConditionOpEnum_EQUAL:
				scOp = "=="
			}

			scValue := ""
			switch sc.GetValue().(type) {
			case *resources.StructuredConditionSpec_Predefined:
				scValue = sc.GetPredefined()
			case *resources.StructuredConditionSpec_UserInput:
				// json marshal the string
				scValue = fmt.Sprintf("%q", sc.GetUserInput())
			}

			var conditionExpr string
			if scOp == "contains" {
				conditionExpr = fmt.Sprintf("%s(%s).contains(%s(%s))", scType, scPath, scType, scValue)
			} else {
				conditionExpr = fmt.Sprintf("%s(%s) %s %s(%s)", scType, scPath, scOp, scType, scValue)
			}
			conditions = append(conditions, conditionExpr)
		}
	}
	result["conditions"] = conditions

	var actions []map[string]interface{}
	for _, actionSpec := range apiRule.GetActionSpecs() {
		switch actionSpecValue := actionSpec.GetSpec().(type) {
		case *resources.ActionSpec_Upload:
			actions = append(actions, map[string]interface{}{
				"name": "upload",
				"kwargs": map[string]interface{}{
					"before":      actionSpecValue.Upload.GetPreTrigger,
					"after":       actionSpecValue.Upload.GetPostTrigger,
					"title":       actionSpecValue.Upload.GetTitle,
					"description": actionSpecValue.Upload.GetDescription,
					"labels":      actionSpecValue.Upload.GetLabels,
					"extra_files": actionSpecValue.Upload.GetExtraFiles,
					"white_list":  actionSpecValue.Upload.GetWhiteList,
				},
			})
		case *resources.ActionSpec_CreateMoment:
			actions = append(actions, map[string]interface{}{
				"name": "create_moment",
				"kwargs": map[string]interface{}{
					"title":         actionSpecValue.CreateMoment.GetTitle,
					"description":   actionSpecValue.CreateMoment.GetDescription,
					"create_task":   actionSpecValue.CreateMoment.GetCreateTask,
					"assign_to":     actionSpecValue.CreateMoment.GetAssignee,
					"custom_fields": actionSpecValue.CreateMoment.GetCustomFields,
				},
			})
		}
	}
	result["actions"] = actions

	result["scopes"] = lo.Map(apiRule.GetEach(), func(each *structpb.Struct, _ int) map[string]string {
		f := each.GetFields()
		vs := make(map[string]string, len(f))
		for k, v := range f {
			vs[k] = v.GetStringValue()
		}
		return vs
	})

	result["topics"] = []string{apiRule.GetActiveTopics()}
	result["condition_debounce"] = apiRule.GetDebounceDuration
	result["version"] = apiRule.GetVersion

	return result
}
