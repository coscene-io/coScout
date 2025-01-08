package rule_engine

import (
	"reflect"
	"testing"
)

func TestAction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		rawKwargs map[string]interface{}
		expected  map[string]interface{}
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
			expected: map[string]interface{}{
				"str_arg":       "hello",
				"int_arg":       123,
				"bool_arg":      true,
				"other_str_arg": "world",
				"dict_arg":      map[string]interface{}{},
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
			expected: map[string]interface{}{
				"str_arg":       "aaa200 bbb",
				"int_arg":       123,
				"bool_arg":      true,
				"other_str_arg": "worl{ ERROR }d",
				"dict_arg":      map[string]interface{}{},
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
			expected: map[string]interface{}{
				"str_arg":       "ccc",
				"int_arg":       123,
				"bool_arg":      true,
				"other_str_arg": "worl{ ERROR }d",
				"dict_arg": map[string]interface{}{
					"aaa": "aaa200 bbb",
					"mmm": 1,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			var result map[string]interface{}
			action, err := createTestAction(c.rawKwargs, &result)
			if err != nil {
				t.Fatalf("Failed to create action: %v", err)
			}

			err = action.Run(getTestActivation())
			if err != nil {
				t.Fatalf("Failed to run action: %v", err)
			}

			if !reflect.DeepEqual(result, c.expected) {
				t.Errorf("Expected %v, got %v", c.expected, result)
			}
		})
	}
}

func createTestAction(rawKwargs map[string]interface{}, result *map[string]interface{}) (*Action, error) {
	return NewAction(
		"serialize",
		rawKwargs,
		func(kwargs map[string]interface{}) error {
			*result = kwargs
			return nil
		},
	)
}

func TestActionValidation(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			var result map[string]interface{}
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
