package rule_engine

import (
	"testing"
)

type actionResult struct {
	StrArg      string
	IntArg      int
	BoolArg     bool
	OtherStrArg string
	DictArg     map[string]interface{}
}

func TestAction(t *testing.T) {
	cases := []struct {
		name      string
		rawKwargs map[string]interface{}
		expected  actionResult
	}{
		{
			name: "simple",
			rawKwargs: map[string]interface{}{
				"str_arg":       "hello",
				"int_arg":       123,
				"bool_arg":      true,
				"other_str_arg": "world",
				"dict_arg":      map[string]interface{}{},
			},
			expected: actionResult{
				StrArg:      "hello",
				IntArg:      123,
				BoolArg:     true,
				OtherStrArg: "world",
				DictArg:     map[string]interface{}{},
			},
		},
		{
			name: "single expression",
			rawKwargs: map[string]interface{}{
				"str_arg":       "aaa{msg.message.code} bbb",
				"int_arg":       123,
				"bool_arg":      true,
				"other_str_arg": "worl{scope.invalid_attr}d",
				"dict_arg":      map[string]interface{}{},
			},
			expected: actionResult{
				StrArg:      "aaa200 bbb",
				IntArg:      123,
				BoolArg:     true,
				OtherStrArg: "worl{ ERROR }d",
				DictArg:     map[string]interface{}{},
			},
		},
		{
			name: "dict",
			rawKwargs: map[string]interface{}{
				"str_arg":       "ccc",
				"int_arg":       123,
				"bool_arg":      true,
				"other_str_arg": "worl{ scope.invalid_attr }d",
				"dict_arg": map[string]interface{}{
					"aaa": "aaa{ msg.message.code } bbb",
					"mmm": 1,
				},
			},
			expected: actionResult{
				StrArg:      "ccc",
				IntArg:      123,
				BoolArg:     true,
				OtherStrArg: "worl{ ERROR }d",
				DictArg: map[string]interface{}{
					"aaa": "aaa200 bbb",
					"mmm": 1,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var result actionResult
			action, err := createTestAction(c.rawKwargs, &result)
			if err != nil {
				t.Fatalf("Failed to create action: %v", err)
			}

			err = action.Run(testActivation)
			if err != nil {
				t.Fatalf("Failed to run action: %v", err)
			}

			if !compareActionResults(result, c.expected) {
				t.Errorf("Expected %v, got %v", c.expected, result)
			}
		})
	}
}

func createTestAction(rawKwargs map[string]interface{}, result *actionResult) (*Action, error) {
	return NewAction(
		"serialize",
		rawKwargs,
		func(kwargs map[string]interface{}) error {
			result.StrArg = kwargs["str_arg"].(string)
			result.IntArg = kwargs["int_arg"].(int)
			result.BoolArg = kwargs["bool_arg"].(bool)
			result.OtherStrArg = kwargs["other_str_arg"].(string)
			result.DictArg = kwargs["dict_arg"].(map[string]interface{})
			return nil
		},
	)
}

func compareActionResults(a, b actionResult) bool {
	return a.StrArg == b.StrArg &&
		a.IntArg == b.IntArg &&
		a.BoolArg == b.BoolArg &&
		a.OtherStrArg == b.OtherStrArg &&
		compareDict(a.DictArg, b.DictArg)
}

func compareDict(dict1 map[string]interface{}, dict2 map[string]interface{}) bool {
	if len(dict1) != len(dict2) {
		return false
	}

	for k, v := range dict1 {
		if v2, ok := dict2[k]; !ok || v != v2 {
			return false
		}
	}

	return true
}

func TestActionDict(t *testing.T) {
	var result actionResult
	rawKwargs := map[string]interface{}{
		"str_arg":       "ccc",
		"int_arg":       123,
		"bool_arg":      true,
		"other_str_arg": "worl{ scope.invalid_attr }d",
		"dict_arg": map[string]interface{}{
			"aaa": "aaa{ msg.message.code } bbb",
			"mmm": 1,
		},
	}

	action, err := createTestAction(rawKwargs, &result)
	if err != nil {
		t.Fatalf("Failed to create action: %v", err)
	}

	err = action.Run(testActivation)
	if err != nil {
		t.Fatalf("Failed to run action: %v", err)
	}

	expected := actionResult{
		StrArg:      "ccc",
		IntArg:      123,
		BoolArg:     true,
		OtherStrArg: "worl{ ERROR }d",
		DictArg: map[string]interface{}{
			"aaa": "aaa200 bbb",
			"mmm": 1,
		},
	}

	if !compareActionResults(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

func TestActionValidation(t *testing.T) {
	tests := []struct {
		name          string
		rawKwargs     map[string]interface{}
		expectSuccess bool
	}{
		{
			name: "valid action",
			rawKwargs: map[string]interface{}{
				"str_arg":       "hello",
				"int_arg":       123,
				"bool_arg":      true,
				"other_str_arg": "world",
				"dict_arg":      map[string]interface{}{},
			},
			expectSuccess: true,
		},
		{
			name: "invalid expression",
			rawKwargs: map[string]interface{}{
				"str_arg":       "hello",
				"int_arg":       123,
				"bool_arg":      true,
				"other_str_arg": "wor{ 1+ }ld",
				"dict_arg":      map[string]interface{}{},
			},
			expectSuccess: false,
		},
		{
			name: "invalid nested dict",
			rawKwargs: map[string]interface{}{
				"str_arg":       "ccc",
				"int_arg":       123,
				"bool_arg":      true,
				"other_str_arg": "worl{ scope.invalid_attr }d",
				"dict_arg": map[string]interface{}{
					"aaa": map[string]interface{}{
						"bbb": 1,
					},
				},
			},
			expectSuccess: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var result actionResult
			_, err := createTestAction(test.rawKwargs, &result)
			if test.expectSuccess && err != nil {
				t.Errorf("Expected success but got error: %v", err)
			}
			if !test.expectSuccess && err == nil {
				t.Error("Expected error but got success")
			}
		})
	}
}
