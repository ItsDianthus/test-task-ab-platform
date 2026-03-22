package main

import (
	"encoding/json"
	"fmt"
	"time"

	"VK_AB_Lotty_task/scripts/demoutil"
)

type decideResponse struct {
	Decisions []struct {
		DecisionID   string          `json:"decision_id"`
		ExperimentID string          `json:"experiment_id"`
		VariantName  string          `json:"variant_name"`
		Value        json.RawMessage `json:"value"`
	} `json:"decisions"`
}

type eventBatchResult struct {
	Accepted  int      `json:"accepted"`
	Duplicate int      `json:"duplicate"`
	Rejected  int      `json:"rejected"`
	Errors    []string `json:"errors"`
}

func main() {
	c := demoutil.NewClient()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	flagKey := "button_color_" + suffix
	expName := "checkout button color " + suffix

	expHeaders := map[string]string{"X-User-ID": "exp", "X-Role": "experimenter"}
	approverHeaders := map[string]string{"X-User-ID": "approver", "X-Role": "approver"}
	adminHeaders := map[string]string{"X-User-ID": "admin", "X-Role": "admin"}

	demoutil.Step("Create flag")
	var flag map[string]interface{}
	demoutil.MustJSON(c, "POST", "/v1/flags", map[string]interface{}{
		"key":           flagKey,
		"type":          "string",
		"default_value": "green",
		"description":   "demo flag",
	}, expHeaders, 201, &flag)

	demoutil.Step("Create experiment")
	var exp map[string]interface{}
	demoutil.MustJSON(c, "POST", "/v1/experiments", map[string]interface{}{
		"flag_key":         flagKey,
		"name":             expName,
		"audience_percent": 100,
		"variants": []map[string]interface{}{
			{"name": "A", "value": "blue", "weight": 50, "is_control": true},
			{"name": "B", "value": "red", "weight": 50, "is_control": false},
		},
		"guardrails": []map[string]interface{}{
			{"metric_key": "error_rate", "threshold": 0.05, "window_seconds": 300, "action": "pause"},
		},
	}, expHeaders, 201, &exp)
	expID := fmt.Sprint(exp["id"])

	demoutil.Step("Review lifecycle")
	demoutil.MustJSON(c, "POST", "/v1/experiments/"+expID+"/submit-review", nil, expHeaders, 200, nil)
	demoutil.MustJSON(c, "POST", "/v1/experiments/"+expID+"/approve", map[string]interface{}{"comment": "safe"}, approverHeaders, 200, nil)
	demoutil.MustJSON(c, "POST", "/v1/experiments/"+expID+"/start", nil, expHeaders, 200, nil)

	demoutil.Step("Deterministic decide")
	req := map[string]interface{}{
		"subject_id": "u42",
		"attributes": map[string]interface{}{"platform": "ios", "country": "SE"},
		"flag_keys":  []string{flagKey},
	}
	var d1, d2 decideResponse
	demoutil.MustJSON(c, "POST", "/v1/decide", req, nil, 200, &d1)
	demoutil.MustJSON(c, "POST", "/v1/decide", req, nil, 200, &d2)
	if len(d1.Decisions) != 1 || len(d2.Decisions) != 1 {
		panic("expected exactly one decision in each call")
	}
	if d1.Decisions[0].VariantName != d2.Decisions[0].VariantName || string(d1.Decisions[0].Value) != string(d2.Decisions[0].Value) {
		panic("determinism check failed: variant/value differ between identical requests")
	}
	decisionID := d1.Decisions[0].DecisionID

	demoutil.Step("Out-of-order events and dedup")
	occ := time.Now().UTC().Format(time.RFC3339Nano)
	var first eventBatchResult
	demoutil.MustJSON(c, "POST", "/v1/events/batch", map[string]interface{}{
		"events": []map[string]interface{}{
			{"event_id": "conv_" + suffix, "type": "conversion", "subject_id": "u42", "decision_id": decisionID, "occurred_at": occ},
		},
	}, nil, 200, &first)
	var exposure eventBatchResult
	demoutil.MustJSON(c, "POST", "/v1/events/batch", map[string]interface{}{
		"events": []map[string]interface{}{
			{"event_id": "exp_" + suffix, "type": "exposure", "subject_id": "u42", "decision_id": decisionID, "occurred_at": occ},
		},
	}, nil, 200, &exposure)
	var duplicate eventBatchResult
	demoutil.MustJSON(c, "POST", "/v1/events/batch", map[string]interface{}{
		"events": []map[string]interface{}{
			{"event_id": "exp_" + suffix, "type": "exposure", "subject_id": "u42", "decision_id": decisionID, "occurred_at": occ},
		},
	}, nil, 200, &duplicate)
	if duplicate.Duplicate < 1 {
		panic("expected duplicate counter for repeated event_id")
	}

	demoutil.Step("Report + audit")
	var report map[string]interface{}
	demoutil.MustJSON(c, "GET", "/v1/reports/"+expID, nil, nil, 200, &report)
	var audit []map[string]interface{}
	demoutil.MustJSON(c, "GET", "/v1/admin/audit/experiment/"+expID, nil, adminHeaders, 200, &audit)

	fmt.Printf("positive scenario passed: flag=%s exp=%s decision=%s audit_entries=%d\n", flagKey, expID, decisionID, len(audit))
}
