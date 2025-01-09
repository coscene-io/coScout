package rule_engine

import (
	"time"

	"github.com/samber/lo"
)

func AllowedVersions() []string {
	return []string{"v2"}
}

// Rule represents a rule with conditions and actions.
type Rule struct {
	Raw                map[string]interface{}
	Conditions         []Condition
	Actions            []Action
	Scope              map[string]string
	Topics             []string
	DebounceTime       time.Duration
	prevActivationTime *time.Time
	Metadata           map[string]interface{}
}

// NewRule creates a new Rule instance.
func NewRule(
	raw map[string]interface{},
	conditions []Condition,
	actions []Action,
	scope map[string]string,
	topics []string,
	debounceTime time.Duration,
	metadata map[string]interface{},
) *Rule {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	return &Rule{
		Raw:          raw,
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

// ValidateRuleSpec validates a single rule specification.
//
//	Example rules specification as follows:
//
//	{
//	  "version": "v2",
//	  "conditions": [
//		   "msg['temperature'] > 20",
//		   "int(msg['humidity']) < int(30)"
//	  ],
//	  "actions: [
//		   {
//			   "name": "action_1",
//			   "kwargs": {
//				   "arg1_1": "{msg['item']}",
//				   "arg1_2": 1
//			   }
//		   },
//		   {
//			   "name": "action_2",
//			   "kwargs": {
//				   "arg2_1": "{msg.code}",
//				   "arg2_2": 1
//			   }
//		   }
//	  ],
//	  "scopes": [
//		   {
//			   "scope_key_a": "scope_value_a_1"
//			   "scope_key_b": "scope_value_b_1"
//		   },
//		   {
//			   "scope_key_a": "scope_value_a_2"
//			   "scope_key_b": "scope_value_b_2"
//		   }
//	  ],
//	  "topics": ["topic_1", "topic_2"]
//	}
func ValidateRuleSpec(ruleSpec map[string]interface{}, actionImpls map[string]interface{}) ([]*Rule, ValidationResult) {
	var errors []ValidationError

	if version, ok := ruleSpec["version"].(string); !ok || !lo.Contains(AllowedVersions(), version) {
		return nil, ValidationResult{
			Success: false,
			Errors: []ValidationError{{
				Location:          nil,
				UnexpectedVersion: &ValidationErrorUnexpectedVersion{AllowedVersions: AllowedVersions()},
			}},
		}
	}

	// Validate conditions
	conditions := make([]Condition, 0)
	conditionsArray, ok := ruleSpec["conditions"].([]string)
	if !ok || len(conditionsArray) == 0 {
		errors = append(errors, ValidationError{
			Location: &ValidationErrorLocation{
				Section: ConditionSection,
			},
			EmptySection: &struct{}{},
		})
	}

	for condIdx, condStr := range conditionsArray {
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

	// Validate actions
	actions := make([]Action, 0)
	actionsArray, ok := ruleSpec["actions"].([]map[string]interface{})
	if !ok || len(actionsArray) == 0 {
		errors = append(errors, ValidationError{
			Location: &ValidationErrorLocation{
				Section: ActionSection,
			},
			EmptySection: &struct{}{},
		})
	}

	for actionIdx, actionSpec := range actionsArray {
		actionName, _ := actionSpec["name"].(string)
		actionKwargs, _ := actionSpec["kwargs"].(map[string]interface{})
		actionImpl, _ := actionImpls[actionName].(func(map[string]interface{}) error)

		action, err := NewAction(actionName, actionKwargs, actionImpl)
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

	// Get scopes and topics
	scopes := ruleSpec["scopes"].([]map[string]string)
	if len(scopes) == 0 {
		scopes = append(scopes, make(map[string]string))
	}

	topics := make([]string, 0)
	if topicsArray, ok := ruleSpec["topics"].([]string); ok {
		topics = topicsArray
	}

	// Create rules for each scope
	var debounceTime time.Duration = 0
	if dt, ok := ruleSpec["condition_debounce"].(string); ok {
		debounceTime, _ = time.ParseDuration(dt)
	}

	rules := make([]*Rule, 0)
	for _, scope := range scopes {
		rules = append(rules, NewRule(
			ruleSpec,
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
