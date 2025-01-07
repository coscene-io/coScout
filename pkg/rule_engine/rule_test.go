package rule_engine

import (
	"testing"
)

func TestValidateRulesSpec(t *testing.T) {
	cases := []struct {
		name     string
		ruleSpec map[string]interface{}
		expected ValidationResult
	}{
		{
			name: "success",
			ruleSpec: map[string]interface{}{
				"version": "v2",
				"rules": []interface{}{
					map[string]interface{}{
						"conditions": []interface{}{
							"msg['temperature'] > 20",
							"msg['humidity'] > 20",
						},
						"actions": []interface{}{
							map[string]interface{}{
								"name": "serialize",
								"kwargs": map[string]interface{}{
									"str_arg": "{msg['item']}",
									"int_arg": 1,
								},
							},
							map[string]interface{}{
								"name": "serialize",
								"kwargs": map[string]interface{}{
									"str_arg": "{msg['item']}",
									"int_arg": 1,
								},
							},
						},
						"scopes": []interface{}{},
						"topics": []string{"test"},
					},
				},
			},
			expected: ValidationResult{
				Success: true,
				Errors:  []ValidationError{},
			},
		},
		{
			name: "empty condition",
			ruleSpec: map[string]interface{}{
				"version": "v2",
				"rules": []interface{}{
					map[string]interface{}{
						"conditions": []interface{}{},
						"actions": []interface{}{
							map[string]interface{}{
								"name": "serialize",
								"kwargs": map[string]interface{}{
									"str_arg": "{msg['item']}",
									"int_arg": 1,
								},
							},
						},
						"scopes": []interface{}{},
						"topics": []string{"test"},
					},
				},
			},
			expected: ValidationResult{
				Success: false,
				Errors: []ValidationError{
					{
						Location: &ValidationErrorLocation{
							Section:   ConditionSection,
							RuleIndex: 0,
							ItemIndex: 0,
						},
						EmptySection: &struct{}{},
					},
				},
			},
		},
		{
			name: "empty action",
			ruleSpec: map[string]interface{}{
				"version": "v2",
				"rules": []interface{}{
					map[string]interface{}{
						"conditions": []interface{}{
							"msg['temperature'] > 20",
							"msg['humidity'] > 20",
						},
						"actions": []interface{}{},
						"scopes":  []interface{}{},
						"topics":  []string{"test"},
					},
				},
			},
			expected: ValidationResult{
				Success: false,
				Errors: []ValidationError{
					{
						Location: &ValidationErrorLocation{
							Section:   ActionSection,
							RuleIndex: 0,
							ItemIndex: 0,
						},
						EmptySection: &struct{}{},
					},
				},
			},
		},
		{
			name: "multiple errors",
			ruleSpec: map[string]interface{}{
				"version": "v2",
				"rules": []interface{}{
					map[string]interface{}{
						"conditions": []interface{}{
							"msg['temperature'] > 20",
							"msg['humidity'] > ",
						},
						"actions": []interface{}{
							map[string]interface{}{
								"name": "serialize",
								"kwargs": map[string]interface{}{
									"str_arg": "{msg['item']}",
									"int_arg": 1,
								},
							},
							map[string]interface{}{
								"name": "serialize",
								"kwargs": map[string]interface{}{
									"str_arg": "{msg[}",
									"int_arg": 1,
								},
							},
							map[string]interface{}{
								"name": "serialize",
								"kwargs": map[string]interface{}{
									"str_arg": "{msg['item']}",
									"int_arg": 1,
								},
							},
						},
						"scopes": []interface{}{},
						"topics": []string{"test"},
					},
				},
			},
			expected: ValidationResult{
				Success: false,
				Errors: []ValidationError{
					{
						Location: &ValidationErrorLocation{
							Section:   ConditionSection,
							RuleIndex: 0,
							ItemIndex: 1,
						},
						SyntaxError: &struct{}{},
					},
					{
						Location: &ValidationErrorLocation{
							Section:   ActionSection,
							RuleIndex: 0,
							ItemIndex: 1,
						},
						SyntaxError: &struct{}{},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, result := ValidateRulesSpec(tc.ruleSpec, map[string]interface{}{
				"serialize": func(map[string]interface{}) error {
					return nil
				},
			})
			if !CompareValidationResult(result, tc.expected) {
				t.Errorf("[%v] Expected %v, got %v", tc.name, tc.expected, result)
			}
		})
	}
}
