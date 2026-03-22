package main

import (
	"fmt"
	"strings"
	"time"

	"VK_AB_Lotty_task/scripts/demoutil"
)

type errorResponse struct {
	Error string `json:"error"`
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

	demoutil.Step("Forbidden: non-admin create user")
	var e1 errorResponse
	demoutil.MustJSON(c, "POST", "/v1/admin/users", map[string]interface{}{
		"id":   "bad-user-" + suffix,
		"role": "viewer",
	}, map[string]string{
		"X-User-ID": "viewer",
		"X-Role":    "viewer",
	}, 403, &e1)

	demoutil.Step("Bad request: invalid role format")
	var e2 errorResponse
	demoutil.MustJSON(c, "POST", "/v1/admin/users", map[string]interface{}{
		"id":   "x-" + suffix,
		"role": "superadmin",
	}, map[string]string{
		"X-User-ID": "admin",
		"X-Role":    "admin",
	}, 400, &e2)

	demoutil.Step("Not found: experiment with unknown flag")
	var e3 errorResponse
	demoutil.MustJSON(c, "POST", "/v1/experiments", map[string]interface{}{
		"flag_key":         "missing-flag-" + suffix,
		"name":             "negative exp " + suffix,
		"audience_percent": 100,
		"variants": []map[string]interface{}{
			{"name": "A", "value": "on", "weight": 100, "is_control": true},
		},
	}, map[string]string{
		"X-User-ID": "exp",
		"X-Role":    "experimenter",
	}, 404, &e3)

	demoutil.Step("Duplicate payload mismatch")
	firstEventID := "dup-" + suffix
	var b1 eventBatchResult
	demoutil.MustJSON(c, "POST", "/v1/events/batch", map[string]interface{}{
		"events": []map[string]interface{}{
			{"event_id": firstEventID, "type": "conversion", "subject_id": "u-neg", "decision_id": "d-missing", "properties": map[string]interface{}{"v": 1}},
		},
	}, nil, 200, &b1)
	var b2 eventBatchResult
	demoutil.MustJSON(c, "POST", "/v1/events/batch", map[string]interface{}{
		"events": []map[string]interface{}{
			{"event_id": firstEventID, "type": "conversion", "subject_id": "u-neg", "decision_id": "d-missing", "properties": map[string]interface{}{"v": 2}},
		},
	}, nil, 200, &b2)
	if b2.Rejected == 0 {
		panic("expected second batch to contain rejection for mismatched duplicate payload")
	}
	found := false
	for _, msg := range b2.Errors {
		if strings.Contains(msg, "different payload") {
			found = true
			break
		}
	}
	if !found {
		panic(fmt.Sprintf("expected duplicate payload mismatch error, got: %v", b2.Errors))
	}

	fmt.Println("negative scenario passed")
}
