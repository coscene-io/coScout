package rule_engine

import (
	"testing"
)

var testActivation = map[string]interface{}{
	"msg": map[string]interface{}{
		"message": map[string]interface{}{
			"code": 200,
		},
		"lst": []interface{}{
			map[string]interface{}{"code": 1},
			map[string]interface{}{"code": 2},
			map[string]interface{}{"code": 3},
		},
	},
	"scope": map[string]interface{}{
		"code": 200,
	},
	"topic": "/TestTopic",
	"ts":    1234567890.123456,
}

func TestConditionIntInt(t *testing.T) {
	tests := []struct {
		condition string
		expected  bool
	}{
		{"1 == 1", true},
		{"1 == 2", false},
	}

	for _, test := range tests {
		condition, err := NewCondition(test.condition)
		if err != nil {
			t.Fatalf("Failed to create condition: %v", err)
		}
		if condition.Evaluate(testActivation) != test.expected {
			t.Errorf("Expected %v for %s", test.expected, test.condition)
		}
	}
}

func TestConditionMsgCastOther(t *testing.T) {
	tests := []struct {
		condition string
		expected  bool
	}{
		{`msg.message.code == int("200")`, true},
		{`msg.message.code == int("201")`, false},
		{`msg.message.code == int(200.0)`, true},
		{`msg.message.code == int(201.0)`, false},
	}

	for _, test := range tests {
		condition, err := NewCondition(test.condition)
		if err != nil {
			t.Fatalf("Failed to create condition: %v", err)
		}
		if condition.Evaluate(testActivation) != test.expected {
			t.Errorf("Expected %v for %s", test.expected, test.condition)
		}
	}
}

func TestConditionMsgScope(t *testing.T) {
	tests := []struct {
		condition string
		expected  bool
	}{
		{`msg.message.code == scope.code`, true},
		{`msg.message.code == scope.invalid_attr`, false},
	}

	for _, test := range tests {
		condition, err := NewCondition(test.condition)
		if err != nil {
			t.Fatalf("Failed to create condition: %v", err)
		}
		if condition.Evaluate(testActivation) != test.expected {
			t.Errorf("Expected %v for %s", test.expected, test.condition)
		}
	}
}

func TestConditionMapContains(t *testing.T) {
	tests := []struct {
		condition string
		expected  bool
	}{
		{`msg.lst.map(x, x.code).exists(y, y==2)`, true},
		{`msg.lst.map(x, x.code).exists(y, y==4)`, false},
		{`msg.lst.map(x, x.code * 2).exists(y, y==6)`, true},
		{`msg.lst.map(x, x.code * 2).exists(y, y==7)`, false},
	}

	for _, test := range tests {
		condition, err := NewCondition(test.condition)
		if err != nil {
			t.Fatalf("Failed to create condition: %v", err)
		}
		if condition.Evaluate(testActivation) != test.expected {
			t.Errorf("Expected %v for %s", test.expected, test.condition)
		}
	}
}

func TestConditionExists(t *testing.T) {
	tests := []struct {
		condition string
		expected  bool
	}{
		{`msg.lst.exists(x, x.code == 2)`, true},
		{`msg.lst.exists(x, x.code == 4)`, false},
		{`msg.lst.exists(x, x.code * 2 == 6)`, true},
		{`msg.lst.exists(x, x.code * 2 == 7)`, false},
	}

	for _, test := range tests {
		condition, err := NewCondition(test.condition)
		if err != nil {
			t.Fatalf("Failed to create condition: %v", err)
		}
		if condition.Evaluate(testActivation) != test.expected {
			t.Errorf("Expected %v for %s", test.expected, test.condition)
		}
	}
}

func TestConditionInvalid(t *testing.T) {
	tests := []string{
		`mmm.message.code == 200`,
		`msg.message.code ==`,
		`msg[0`,
	}

	for _, test := range tests {
		_, err := NewCondition(test)
		if err == nil {
			t.Errorf("Expected error for condition: %s", test)
		}
	}
}
