package domain

import "testing"

func TestEvaluateRuleComplexExpression(t *testing.T) {
	rule := &RuleNode{
		Op: "AND",
		Children: []RuleNode{
			{Op: "==", Field: "country", Value: "SE"},
			{
				Op: "OR",
				Children: []RuleNode{
					{Op: ">=", Field: "age", Value: 18},
					{Op: "==", Field: "vip", Value: true},
				},
			},
		},
	}

	ok, err := EvaluateRule(rule, map[string]interface{}{
		"country": "SE",
		"age":     21,
		"vip":     false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected rule to match")
	}
}

func TestEvaluateRuleNotOperator(t *testing.T) {
	rule := &RuleNode{
		Op: "NOT",
		Children: []RuleNode{
			{Op: "==", Field: "platform", Value: "android"},
		},
	}
	ok, err := EvaluateRule(rule, map[string]interface{}{"platform": "ios"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected NOT rule to match")
	}
}

func TestEvaluateRuleInAndDateComparison(t *testing.T) {
	rule := &RuleNode{
		Op:    "AND",
		Field: "",
		Children: []RuleNode{
			{Op: "IN", Field: "segment", Values: []interface{}{"a", "b"}},
			{Op: ">", Field: "signup_date", Value: "2024-12-31"},
		},
	}
	ok, err := EvaluateRule(rule, map[string]interface{}{
		"segment":     "b",
		"signup_date": "2025-01-10",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected IN+date rule to match")
	}
}

func TestEvaluateRuleInvalidNotArity(t *testing.T) {
	rule := &RuleNode{Op: "NOT"}
	_, err := EvaluateRule(rule, map[string]interface{}{})
	if err == nil {
		t.Fatalf("expected NOT arity error")
	}
}
