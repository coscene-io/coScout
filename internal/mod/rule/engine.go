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

package rule

import (
	"encoding/json"
	"strconv"

	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/internal/name"
	"github.com/coscene-io/coscout/pkg/rule_engine"
	"github.com/coscene-io/coscout/pkg/utils"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	log "github.com/sirupsen/logrus"
	"github.com/sosodev/duration"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// Engine represents the rule engine that processes messages against rules.
type Engine struct {
	reqClient  api.RequestClient
	deviceName string

	rules        []*rule_engine.Rule
	activeTopics mapset.Set[string]
}

// UpdateRules updates the rules in the rule engine.
func (e *Engine) UpdateRules(apiRules []*resources.DiagnosisRule, configTopics []string) {
	var (
		rules        []*rule_engine.Rule
		activeTopics = mapset.NewSet[string]()
	)
	for _, apiRule := range apiRules {
		validatedRules, validationResult := rule_engine.ValidateApiRule(
			apiRule,
			map[string]rule_engine.ActionImpl{
				"upload":        e.uploadActionImpl,
				"create_moment": createMomentActionImpl,
			},
		)
		if !validationResult.Success {
			log.Errorf("rule validation failed for rule: %s, skipping", apiRule.GetDisplayName())
			bytes, err := json.Marshal(validationResult)
			if err == nil {
				log.Errorf("validation result: %s", string(bytes))
			}

			continue
		}

		diagnosisRuleName, err := name.NewDiagnosisRule(apiRule.GetName())
		if err != nil {
			log.Errorf("failed to parse diagnosis rule name: %v, skipping", apiRule.GetName())
			continue
		}

		// Add metadata to validated rules
		for _, validatedRule := range validatedRules {
			// check if rule has an upload action
			if !lo.SomeBy(validatedRule.Actions, func(action rule_engine.Action) bool {
				return action.Name == "upload"
			}) {
				log.Debugf("rule %s does not have upload action, skipping", apiRule.GetName())
				continue
			}

			validatedRule.Metadata["project_name"] = diagnosisRuleName.Project().String()
			validatedRule.Metadata["rule_name"] = diagnosisRuleName.String()
			if code, ok := validatedRule.Scope["code"]; ok {
				validatedRule.Metadata["rule_code"] = code
			} else {
				validatedRule.Metadata["rule_code"] = ""
			}
			validatedRule.Metadata["rule_display_name"] = apiRule.GetDisplayName()

			rule_with_current_scope, _ := proto.Clone(apiRule).(*resources.DiagnosisRule)
			fields := map[string]*structpb.Value{}
			for k, v := range validatedRule.Scope {
				fields[k] = structpb.NewStringValue(v)
			}
			rule_with_current_scope.SetEach([]*structpb.Struct{{Fields: fields}})
			validatedRule.Metadata["rule"] = rule_with_current_scope

			rules = append(rules, validatedRule)

			activeTopics = activeTopics.Union(validatedRule.Topics)
		}
	}
	e.rules = rules

	configTopicSet := mapset.NewSet(configTopics...)
	e.activeTopics = activeTopics.Intersect(configTopicSet)
	e.activeTopics.Add("/external_log")

	log.Infof("Updated %d valid rules", len(rules))
}

// ActiveTopics returns all the active topics in the rule engine.
func (e *Engine) ActiveTopics() mapset.Set[string] {
	return e.activeTopics
}

// ConsumeNext shows how to process a message through the rule engine.
func (e *Engine) ConsumeNext(item rule_engine.RuleItem) {
	for _, rule := range e.rules {
		if !rule.Topics.Contains(item.Topic) {
			continue
		}

		curActivation := map[string]interface{}{
			"msg":   item.Msg,
			"scope": rule.Scope,
			"topic": item.Topic,
			"ts":    item.Ts,
		}

		if rule.EvalConditions(curActivation, utils.TimeFromFloat(item.Ts)) {
			collectInfoId := uuid.New().String()
			additionalArgs := map[string]interface{}{}
			for k, v := range rule.Metadata {
				additionalArgs[k] = v
			}
			additionalArgs["collect_info_id"] = collectInfoId

			for _, action := range rule.Actions {
				if err := action.Run(curActivation, additionalArgs); err != nil {
					log.Errorf("failed to run action %s: %v", action.Name, err)
				}
			}
		}
	}
}

// uploadActionImpl is the implementation of "upload" function,
// it stores the collect info in the database.
func (e *Engine) uploadActionImpl(kwargs map[string]interface{}) error {
	log.Infof("triggered saving collect info")

	// Extract required fields with type assertions
	beforeStr, ok := kwargs["before"].(string)
	if !ok {
		return errors.Errorf("before must be a string")
	}
	before, err := duration.Parse(beforeStr)
	if err != nil {
		return errors.Wrap(err, "failed to parse before to duration")
	}

	afterStr, ok := kwargs["after"].(string)
	if !ok {
		return errors.Errorf("after must be a string")
	}
	after, err := duration.Parse(afterStr)
	if err != nil {
		return errors.Wrap(err, "failed to parse after to duration")
	}

	title, ok := kwargs["title"].(string)
	if !ok {
		return errors.Errorf("title must be a string")
	}

	description, ok := kwargs["description"].(string)
	if !ok {
		return errors.Errorf("description must be a string")
	}

	labels, ok := kwargs["labels"].([]string)
	if !ok {
		return errors.Errorf("labels must be a list of strings")
	}

	extraFiles, ok := kwargs["extra_files"].([]string)
	if !ok {
		return errors.Errorf("extra_files must be a list of strings")
	}

	whiteList, ok := kwargs["white_list"].([]string)
	if !ok {
		return errors.Errorf("white_list must be a list of strings")
	}

	triggerTsStr, ok := kwargs["ts"].(string)
	if !ok {
		return errors.Errorf("ts must be a string")
	}
	triggerTsFloat, err := strconv.ParseFloat(triggerTsStr, 64)
	if err != nil {
		return errors.Wrap(err, "failed to parse ts to float")
	}
	triggerTs := utils.TimeFromFloat(triggerTsFloat)

	projectName, ok := kwargs["project_name"].(string)
	if !ok {
		return errors.Errorf("project_name must be a string")
	}

	ruleName, ok := kwargs["rule_name"].(string)
	if !ok {
		return errors.Errorf("rule_name must be a string")
	}

	ruleDisplayName, ok := kwargs["rule_display_name"].(string)
	if !ok {
		return errors.Errorf("rule_display_name must be a string")
	}

	collectInfoId, ok := kwargs["collect_info_id"].(string)
	if !ok {
		return errors.Errorf("collect_info_id must be a string")
	}

	uploadLimit, ok := kwargs["upload_limit"].(*resources.UploadLimit)
	if !ok {
		return errors.Errorf("upload_limit must be a UploadLimit object")
	}

	rule, ok := kwargs["rule"].(*resources.DiagnosisRule)
	if !ok {
		return errors.Errorf("rule must be a DiagnosisRule object")
	}

	canUpload := e.canUpload(uploadLimit, rule)
	if err := e.reqClient.HitDiagnosisRule(rule, e.deviceName, canUpload); err != nil {
		return errors.Wrap(err, "failed to hit diagnosis rule")
	}

	// Calculate start and end times
	startTime := triggerTs.Add(-before.ToTimeDuration())
	endTime := triggerTs.Add(after.ToTimeDuration())

	// Create collect info
	collectInfo := &model.CollectInfo{Id: collectInfoId}
	if err := collectInfo.Load(collectInfoId); err != nil {
		*collectInfo = model.CollectInfo{Id: collectInfoId}
	}

	collectInfo.ProjectName = projectName
	collectInfo.Record = map[string]interface{}{
		"title":       title,
		"description": description,
	}
	collectInfo.Labels = labels
	collectInfo.DiagnosisTask = map[string]interface{}{
		"rule_name":         ruleName,
		"rule_display_name": ruleDisplayName,
		"trigger_time":      utils.FloatSecFromTime(triggerTs),
		"start_time":        utils.FloatSecFromTime(startTime),
		"end_time":          utils.FloatSecFromTime(endTime),
	}
	collectInfo.Cut = &model.CollectInfoCut{
		ExtraFiles: extraFiles,
		Start:      startTime.Unix(),
		End:        endTime.Unix(),
		WhiteList:  whiteList,
	}
	collectInfo.Skip = !canUpload

	// Save the collect info
	if err = collectInfo.Save(); err != nil {
		return errors.Wrap(err, "failed to save collect info")
	}
	log.Infof("saved collect info %s", collectInfoId)

	return nil
}

func (e *Engine) canUpload(uploadLimit *resources.UploadLimit, rule *resources.DiagnosisRule) bool {
	if uploadLimit.HasDevice() {
		count, err := e.reqClient.CountDiagnosisRuleHits(rule, e.deviceName)
		if err != nil {
			log.Errorf("failed to count diagnosis rule hits: %v, skipping", err)
			return false
		}

		log.Infof("device count: %d, limit: %d", count, uploadLimit.GetDevice().GetTimes())

		if count >= uploadLimit.GetDevice().GetTimes() {
			log.Infof("device count %d exceeds limit %d, skipping", count, uploadLimit.GetDevice().GetTimes())
			return false
		}
	}

	if uploadLimit.HasGlobal() {
		count, err := e.reqClient.CountDiagnosisRuleHits(rule, "")
		if err != nil {
			log.Errorf("failed to count diagnosis rule hits: %v, skipping", err)
			return false
		}

		log.Infof("global count: %d, limit: %d", count, uploadLimit.GetGlobal().GetTimes())

		if count >= uploadLimit.GetGlobal().GetTimes() {
			log.Infof("global count %d exceeds limit %d, skipping", count, uploadLimit.GetGlobal().GetTimes())
			return false
		}
	}
	return true
}

// createMomentActionImpl is the implementation of "create_moment" function,
// it adds moment to the collect info in the database.
func createMomentActionImpl(kwargs map[string]interface{}) error {
	log.Infof("triggered creating moment")

	// Extract required fields with type assertions
	collectInfoId, ok := kwargs["collect_info_id"].(string)
	if !ok {
		return errors.Errorf("collect_info_id must be a string")
	}

	title, ok := kwargs["title"].(string)
	if !ok {
		return errors.Errorf("title must be a string")
	}

	description, ok := kwargs["description"].(string)
	if !ok {
		return errors.Errorf("description must be a string")
	}

	createTask, ok := kwargs["create_task"].(bool)
	if !ok {
		return errors.Errorf("create_task must be a boolean")
	}

	syncTask, ok := kwargs["sync_task"].(bool)
	if !ok {
		return errors.Errorf("sync_task must be a boolean")
	}

	assignee, ok := kwargs["assignee"].(string)
	if !ok {
		return errors.Errorf("assignee must be a string")
	}

	customFields, ok := kwargs["custom_fields"].(map[string]interface{})
	if !ok {
		return errors.Errorf("custom_fields must be a map")
	}
	customFieldsString := map[string]string{}
	for k, v := range customFields {
		if s, ok := v.(string); ok {
			customFieldsString[k] = s
		}
	}

	ruleCode, ok := kwargs["rule_code"].(string)
	if !ok {
		return errors.Errorf("rule_code must be a string")
	}

	triggerTsStr, ok := kwargs["ts"].(string)
	if !ok {
		return errors.Errorf("ts must be a string")
	}
	triggerTsFloat, err := strconv.ParseFloat(triggerTsStr, 64)
	if err != nil {
		return errors.Wrap(err, "failed to parse ts to float")
	}
	triggerTs := utils.TimeFromFloat(triggerTsFloat)

	// Create collect info
	collectInfo := &model.CollectInfo{Id: collectInfoId}
	if err := collectInfo.Load(collectInfoId); err != nil {
		*collectInfo = model.CollectInfo{Id: collectInfoId}
	}
	collectInfo.Moments = []model.CollectInfoMoment{
		{
			Title:        title,
			Description:  description,
			Timestamp:    utils.FloatSecFromTime(triggerTs),
			StartTime:    utils.FloatSecFromTime(triggerTs),
			CustomFields: customFieldsString,
			Code:         ruleCode,
			CreateTask:   createTask,
			SyncTask:     syncTask,
			AssignTo:     assignee,
		},
	}

	// Save the collect info
	if err = collectInfo.Save(); err != nil {
		return errors.Wrap(err, "failed to save collect info")
	}
	log.Infof("saved collect info %s", collectInfoId)

	return nil
}
