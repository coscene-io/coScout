package rule_engine

import (
	"testing"
	"time"
)

func TestEngineComprehensiveSingleCondition(t *testing.T) {
	t.Parallel()

	var result map[string]interface{}
	engine := createTestEngine(t,
		map[string]interface{}{
			"version": "v2",
			"conditions": []string{
				"msg.code > 20",
			},
			"actions": []map[string]interface{}{
				{
					"name": "serialize",
					"kwargs": map[string]interface{}{
						"str_arg": "{msg.code}",
						"int_arg": 1,
					},
				},
			},
			"scopes": []map[string]string{},
			"topics": []string{"test_topic"},
		}, &result)

	testCases := []struct {
		msg      map[string]interface{}
		expected map[string]interface{}
	}{
		{
			msg:      map[string]interface{}{"code": 20},
			expected: nil,
		},
		{
			msg: map[string]interface{}{"code": 21},
			expected: map[string]interface{}{
				"str_arg": "21",
				"int_arg": 1,
			},
		},
		{
			msg: map[string]interface{}{"code": 22},
			expected: map[string]interface{}{
				"str_arg": "22",
				"int_arg": 1,
			},
		},
		{
			msg: map[string]interface{}{"code": 20},
			expected: map[string]interface{}{
				"str_arg": "22",
				"int_arg": 1,
			},
		},
	}

	for i, tc := range testCases {
		err := engine.ExampleConsumeNext(tc.msg, "test_topic", time.Now())
		if err != nil {
			t.Fatalf("Test case %d: Failed to consume message: %v", i, err)
		}

		if tc.expected == nil && result != nil {
			t.Errorf("Test case %d: Expected no result, got %v", i, result)
		} else if tc.expected != nil {
			if result["str_arg"] != tc.expected["str_arg"] || result["int_arg"] != tc.expected["int_arg"] {
				t.Errorf("Test case %d: Expected %v, got %v", i, tc.expected, result)
			}
		}
	}
}

func TestEngineComprehensiveMultipleCondition(t *testing.T) {
	t.Parallel()

	var result map[string]interface{}
	engine := createTestEngine(t, map[string]interface{}{
		"version": "v2",
		"conditions": []string{
			"msg.code > 20",
			"int(msg.level) <= int(3)",
		},
		"actions": []map[string]interface{}{
			{
				"name": "serialize",
				"kwargs": map[string]interface{}{
					"str_arg": "{msg.code}",
					"int_arg": 1,
				},
			},
		},
		"scopes": []map[string]string{},
		"topics": []string{"test_topic"},
	}, &result)

	testCases := []struct {
		msg      map[string]interface{}
		expected map[string]interface{}
	}{
		{
			msg:      map[string]interface{}{"code": 20, "level": "4"},
			expected: nil,
		},
		{
			msg:      map[string]interface{}{"code": 19, "level": "2"},
			expected: nil,
		},
		{
			msg:      map[string]interface{}{"code": 19, "level": "4"},
			expected: nil,
		},
		{
			msg: map[string]interface{}{"code": 21, "level": "2"},
			expected: map[string]interface{}{
				"str_arg": "21",
				"int_arg": 1,
			},
		},
		{
			msg: map[string]interface{}{"code": 23, "level": "1"},
			expected: map[string]interface{}{
				"str_arg": "23",
				"int_arg": 1,
			},
		},
		{
			msg: map[string]interface{}{"code": 21, "level": "4"},
			expected: map[string]interface{}{
				"str_arg": "23",
				"int_arg": 1,
			},
		},
		{
			msg: map[string]interface{}{"code": 19, "level": "2"},
			expected: map[string]interface{}{
				"str_arg": "23",
				"int_arg": 1,
			},
		},
		{
			msg: map[string]interface{}{"code": 19, "level": "4"},
			expected: map[string]interface{}{
				"str_arg": "23",
				"int_arg": 1,
			},
		},
	}

	for i, tc := range testCases {
		err := engine.ExampleConsumeNext(tc.msg, "test_topic", time.Now())
		if err != nil {
			t.Fatalf("Test case %d: Failed to consume message: %v", i, err)
		}

		if tc.expected == nil && result != nil {
			t.Errorf("Test case %d: Expected no result, got %v", i, result)
		} else if tc.expected != nil {
			if result["str_arg"] != tc.expected["str_arg"] || result["int_arg"] != tc.expected["int_arg"] {
				t.Errorf("Test case %d: Expected %v, got %v", i, tc.expected, result)
			}
		}
	}
}

func TestEngineScope(t *testing.T) {
	t.Parallel()

	var result map[string]interface{}
	engine := createTestEngine(t, map[string]interface{}{
		"version": "v2",
		"conditions": []string{
			"msg.code > 20",
			"int(scope.level) <= int(3)",
		},
		"actions": []map[string]interface{}{
			{
				"name": "serialize",
				"kwargs": map[string]interface{}{
					"str_arg": "{scope.code}",
					"int_arg": 1,
				},
			},
		},
		"scopes": []map[string]string{
			{
				"code":  "77",
				"level": "1",
			},
		},
		"topics": []string{"test_topic"},
	}, &result)

	testCases := []struct {
		msg      map[string]interface{}
		expected map[string]interface{}
	}{
		{
			msg: map[string]interface{}{"code": 21},
			expected: map[string]interface{}{
				"str_arg": "77",
				"int_arg": 1,
			},
		},
	}

	for i, tc := range testCases {
		err := engine.ExampleConsumeNext(tc.msg, "test_topic", time.Now())
		if err != nil {
			t.Fatalf("Test case %d: Failed to consume message: %v", i, err)
		}

		if tc.expected == nil && result != nil {
			t.Errorf("Test case %d: Expected no result, got %v", i, result)
		} else if tc.expected != nil {
			if result["str_arg"] != tc.expected["str_arg"] || result["int_arg"] != tc.expected["int_arg"] {
				t.Errorf("Test case %d: Expected %v, got %v", i, tc.expected, result)
			}
		}
	}
}

func createTestEngine(t *testing.T, rulesSpec map[string]interface{}, result *map[string]interface{}) *Engine {
	t.Helper()

	actionImpls := map[string]interface{}{
		"serialize": func(kwargs map[string]interface{}) error {
			*result = kwargs
			return nil
		},
	}

	rules, validationResult := ValidateRuleSpec(rulesSpec, actionImpls)
	if !validationResult.Success {
		t.Fatalf("Failed to validate rules: %v", validationResult.Errors)
	}

	return NewEngine(rules)
}
