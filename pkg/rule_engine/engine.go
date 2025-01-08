package rule_engine

import (
	"time"
)

// Engine represents the rule engine that processes messages against rules.
type Engine struct {
	rules         []*Rule
	curActivation map[string]interface{}
}

// NewEngine creates a new rule engine instance.
func NewEngine(rules []*Rule) *Engine {
	return &Engine{
		rules: rules,
	}
}

// LoadMessage prepares a message for processing.
func (e *Engine) LoadMessage(msg map[string]interface{}, topic string, ts time.Time) {
	e.curActivation = map[string]interface{}{
		"msg":   msg,
		"topic": topic,
		"ts":    float64(ts.Unix()),
	}
}

// EvaluateRuleCondition evaluates conditions for a specific rule.
func (e *Engine) EvaluateRuleCondition(ruleIdx int) bool {
	if ruleIdx >= len(e.rules) {
		return false
	}

	rule := e.rules[ruleIdx]
	activation := e.curActivation
	activation["scope"] = rule.Scope

	return rule.EvalConditions(activation, time.Now())
}

// RunRuleActions executes actions for a specific rule.
func (e *Engine) RunRuleActions(ruleIdx int) error {
	if ruleIdx >= len(e.rules) {
		return nil
	}

	rule := e.rules[ruleIdx]
	activation := e.curActivation
	activation["scope"] = rule.Scope

	for _, action := range rule.Actions {
		if err := action.Run(activation); err != nil {
			return err
		}
	}

	return nil
}

// ExampleConsumeNext shows how to process a message through the rule engine.
func (e *Engine) ExampleConsumeNext(msg map[string]interface{}, topic string, ts time.Time) error {
	e.LoadMessage(msg, topic, ts)

	for i := range e.rules {
		if e.EvaluateRuleCondition(i) {
			if err := e.RunRuleActions(i); err != nil {
				return err
			}
		}
	}

	return nil
}
