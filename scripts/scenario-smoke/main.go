package main

import (
	"fmt"
	"time"

	"VK_AB_Lotty_task/scripts/demoutil"
)

func main() {
	c := demoutil.NewClient()
	demoutil.Step("Smoke: health and ready")
	var health map[string]string
	demoutil.MustJSON(c, "GET", "/health", nil, nil, 200, &health)
	var ready map[string]string
	demoutil.MustJSON(c, "GET", "/ready", nil, nil, 200, &ready)
	fmt.Printf("health=%s ready=%s\n", health["status"], ready["status"])

	demoutil.Step("Smoke: list endpoints")
	var flags []map[string]interface{}
	demoutil.MustJSON(c, "GET", "/v1/flags", nil, nil, 200, &flags)
	var eventTypes []map[string]interface{}
	demoutil.MustJSON(c, "GET", "/v1/admin/event-types", nil, map[string]string{
		"X-User-ID": "admin",
		"X-Role":    "admin",
	}, 200, &eventTypes)
	fmt.Printf("flags=%d event_types=%d ts=%s\n", len(flags), len(eventTypes), time.Now().UTC().Format(time.RFC3339))
	fmt.Println("smoke scenario passed")
}
