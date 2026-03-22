package domain

import (
	"fmt"
	"strings"
	"time"
)

func EvaluateRule(rule *RuleNode, attrs map[string]interface{}) (bool, error) {
	if rule == nil {
		return true, nil
	}
	op := strings.ToUpper(strings.TrimSpace(rule.Op))
	switch op {
	case "AND":
		for _, child := range rule.Children {
			ok, err := EvaluateRule(&child, attrs)
			if err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	case "OR":
		for _, child := range rule.Children {
			ok, err := EvaluateRule(&child, attrs)
			if err == nil && ok {
				return true, nil
			}
		}
		return false, nil
	case "NOT":
		if len(rule.Children) != 1 {
			return false, fmt.Errorf("NOT expects 1 child")
		}
		ok, err := EvaluateRule(&rule.Children[0], attrs)
		if err != nil {
			return false, err
		}
		return !ok, nil
	case "==", "!=", ">", ">=", "<", "<=", "IN", "NOT IN":
		v, ok := attrs[rule.Field]
		if !ok {
			return false, nil
		}
		return compare(op, v, rule.Value, rule.Values)
	default:
		return false, fmt.Errorf("unsupported op %q", op)
	}
}

func compare(op string, left interface{}, right interface{}, rights []interface{}) (bool, error) {
	switch op {
	case "IN", "NOT IN":
		found := false
		for _, rv := range rights {
			eq, _ := compare("==", left, rv, nil)
			if eq {
				found = true
				break
			}
		}
		if op == "IN" {
			return found, nil
		}
		return !found, nil
	}

	lf, lok := toFloat(left)
	rf, rok := toFloat(right)
	if lok && rok {
		switch op {
		case "==":
			return lf == rf, nil
		case "!=":
			return lf != rf, nil
		case ">":
			return lf > rf, nil
		case ">=":
			return lf >= rf, nil
		case "<":
			return lf < rf, nil
		case "<=":
			return lf <= rf, nil
		}
	}

	ls, lok := left.(string)
	rs, rok := right.(string)
	if lok && rok {
		// Keep date support lightweight and deterministic.
		if ld, err := time.Parse("2006-01-02", ls); err == nil {
			if rd, err2 := time.Parse("2006-01-02", rs); err2 == nil {
				switch op {
				case "==":
					return ld.Equal(rd), nil
				case "!=":
					return !ld.Equal(rd), nil
				case ">":
					return ld.After(rd), nil
				case ">=":
					return ld.After(rd) || ld.Equal(rd), nil
				case "<":
					return ld.Before(rd), nil
				case "<=":
					return ld.Before(rd) || ld.Equal(rd), nil
				}
			}
		}
		switch op {
		case "==":
			return ls == rs, nil
		case "!=":
			return ls != rs, nil
		case ">":
			return ls > rs, nil
		case ">=":
			return ls >= rs, nil
		case "<":
			return ls < rs, nil
		case "<=":
			return ls <= rs, nil
		}
	}

	lb, lok := left.(bool)
	rb, rok := right.(bool)
	if lok && rok {
		switch op {
		case "==":
			return lb == rb, nil
		case "!=":
			return lb != rb, nil
		default:
			return false, nil
		}
	}
	return false, nil
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}
