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
	DebounceTime       int
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
	debounceTime int,
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
	case ts.Sub(*r.prevActivationTime) > time.Duration(r.DebounceTime)*time.Second:
		activated = true
	default:
		activated = false
	}

	r.prevActivationTime = &ts
	return activated
}

// ValidateRulesSpec validates a rule specification and returns the compiled rules.
func ValidateRulesSpec(rulesSpec map[string]interface{}, actionImpls map[string]interface{}) ([]*Rule, ValidationResult) {
	var allRules []*Rule
	var errors []ValidationError

	version, ok := rulesSpec["version"].(string)
	if !ok || !lo.Contains(AllowedVersions(), version) {
		return nil, ValidationResult{
			Success: false,
			Errors: []ValidationError{{
				UnexpectedVersion: &ValidationErrorUnexpectedVersion{AllowedVersions: AllowedVersions()},
			}},
		}
	}

	rulesArray, ok := rulesSpec["rules"].([]interface{})
	if !ok {
		return nil, ValidationResult{
			Success: false,
			Errors: []ValidationError{{
				EmptySection: &struct{}{},
			}},
		}
	}

	for ruleIdx, ruleInterface := range rulesArray {
		ruleSpec, ok := ruleInterface.(map[string]interface{})
		if !ok {
			continue
		}
		rules, errs := validateRuleSpec(ruleSpec, actionImpls, ruleIdx)
		allRules = append(allRules, rules...)
		errors = append(errors, errs...)
	}

	return allRules, ValidationResult{
		Success: len(errors) == 0,
		Errors:  errors,
	}
}

// validateRuleSpec validates a single rule specification.
func validateRuleSpec(ruleSpec map[string]interface{}, actionImpls map[string]interface{}, ruleIdx int) ([]*Rule, []ValidationError) {
	var errors []ValidationError

	// Validate conditions
	conditions := make([]Condition, 0)
	conditionsArray, ok := ruleSpec["conditions"].([]interface{})
	if !ok || len(conditionsArray) == 0 {
		errors = append(errors, ValidationError{
			Location: &ValidationErrorLocation{
				RuleIndex: ruleIdx,
				Section:   ConditionSection,
			},
			EmptySection: &struct{}{},
		})
	}

	for condIdx, condInterface := range conditionsArray {
		condStr, ok := condInterface.(string)
		if !ok {
			continue
		}
		condition, err := NewCondition(condStr)
		if err != nil {
			errors = append(errors, ValidationError{
				Location: &ValidationErrorLocation{
					RuleIndex: ruleIdx,
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
	actionsArray, ok := ruleSpec["actions"].([]interface{})
	if !ok || len(actionsArray) == 0 {
		errors = append(errors, ValidationError{
			Location: &ValidationErrorLocation{
				RuleIndex: ruleIdx,
				Section:   ActionSection,
			},
			EmptySection: &struct{}{},
		})
	}

	for actionIdx, actionInterface := range actionsArray {
		actionSpec, ok := actionInterface.(map[string]interface{})
		if !ok {
			continue
		}

		actionName, _ := actionSpec["name"].(string)
		actionKwargs, _ := actionSpec["kwargs"].(map[string]interface{})
		actionImpl, _ := actionImpls[actionName].(func(map[string]interface{}) error)

		action, err := NewAction(actionName, actionKwargs, actionImpl)
		if err != nil {
			errors = append(errors, ValidationError{
				Location: &ValidationErrorLocation{
					RuleIndex: ruleIdx,
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
		return nil, errors
	}

	// Get scopes and topics
	scopes := make([]map[string]string, 0)
	if scopesArray, ok := ruleSpec["scopes"].([]interface{}); ok {
		for _, scopeInterface := range scopesArray {
			if scopeMap, ok := scopeInterface.(map[string]interface{}); ok {
				scope := make(map[string]string)
				for k, v := range scopeMap {
					if strVal, ok := v.(string); ok {
						scope[k] = strVal
					}
				}
				scopes = append(scopes, scope)
			}
		}
	}
	if len(scopes) == 0 {
		scopes = append(scopes, make(map[string]string))
	}

	topics := make([]string, 0)
	if topicsArray, ok := ruleSpec["topics"].([]string); ok {
		topics = topicsArray
	}

	// Create rules for each scope
	debounceTime := 0
	if dt, ok := ruleSpec["condition_debounce"].(int); ok {
		debounceTime = dt
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

	return rules, nil
}
