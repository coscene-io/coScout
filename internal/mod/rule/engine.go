package rule

import (
	"strconv"

	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/internal/name"
	"github.com/coscene-io/coscout/pkg/rule_engine"
	"github.com/coscene-io/coscout/pkg/utils"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	log "github.com/sirupsen/logrus"
)

// Engine represents the rule engine that processes messages against rules.
type Engine struct {
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
				"upload":        uploadActionImpl,
				"create_moment": rule_engine.EmptyActionImpl,
			},
		)
		if !validationResult.Success {
			log.Errorf("rule validation failed for rule: %v, skipping", apiRule)
			continue
		}

		diagnosisRuleName, err := name.NewDiagnosisRule(apiRule.GetName())
		if err != nil {
			log.Errorf("failed to parse diagnosis rule name: %v, skipping", apiRule.GetName())
			continue
		}

		// Add metadata to validated rules
		for _, validatedRule := range validatedRules {
			uploadAction, hasUpload := lo.First(lo.Filter(validatedRule.Actions, func(action rule_engine.Action, _ int) bool {
				return action.Name == "upload"
			}))
			if !hasUpload {
				log.Debugf("rule %s does not have upload action, skipping", apiRule.GetName())
				continue
			}
			validatedRule.Actions = []rule_engine.Action{uploadAction}

			createMomentAction, hasCreateMoment := lo.First(lo.Filter(validatedRule.Actions, func(action rule_engine.Action, _ int) bool {
				return action.Name == "create_moment"
			}))
			if hasCreateMoment {
				validatedRule.Actions = []rule_engine.Action{
					uploadAction,
					createMomentAction,
				}
			}

			validatedRule.Metadata["project_name"] = diagnosisRuleName.Project().String()
			validatedRule.Metadata["rule_id"] = diagnosisRuleName.Id
			validatedRule.Metadata["rule_display_name"] = apiRule.GetDisplayName()
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
func uploadActionImpl(kwargs map[string]interface{}) error {
	log.Infof("triggered saving collect info")

	// Extract required fields with type assertions
	beforeStr, ok := kwargs["before"].(string)
	if !ok {
		return errors.Errorf("before must be a string")
	}
	before, err := utils.ParseISODuration(beforeStr)
	if err != nil {
		return errors.Wrap(err, "failed to parse before to duration")
	}

	afterStr, ok := kwargs["after"].(string)
	if !ok {
		return errors.Errorf("after must be a string")
	}
	after, err := utils.ParseISODuration(afterStr)
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

	ruleId, ok := kwargs["rule_id"].(string)
	if !ok {
		return errors.Errorf("rule_id must be a string")
	}

	ruleDisplayName, ok := kwargs["rule_display_name"].(string)
	if !ok {
		return errors.Errorf("rule_display_name must be a string")
	}

	collectInfoId, ok := kwargs["collect_info_id"].(string)
	if !ok {
		return errors.Errorf("collect_info_id must be a string")
	}

	// Calculate start and end times
	startTime := triggerTs.Add(-before)
	endTime := triggerTs.Add(after)

	// Create collect info
	collectInfo := &model.CollectInfo{
		ProjectName: projectName,
		Record: map[string]interface{}{
			"title":       title,
			"description": description,
			"labels":      labels,
			"rules": []map[string]interface{}{
				{"id": ruleId},
			},
		},
		DiagnosisTask: map[string]interface{}{
			"rule_id":           ruleId,
			"rule_display_name": ruleDisplayName,
			"trigger_time":      triggerTs.Unix(),
			"start_time":        startTime.Unix(),
			"end_time":          endTime.Unix(),
		},
		Cut: &model.CollectInfoCut{
			ExtraFiles: extraFiles,
			Start:      startTime.Unix(),
			End:        endTime.Unix(),
			WhiteList:  whiteList,
		},
		Id: collectInfoId,
	}

	// Save the collect info
	defer log.Infof("saved collect info %s", collectInfoId)
	return collectInfo.Save()
}
