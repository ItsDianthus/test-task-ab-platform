package demoutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func NewClient() *Client {
	base := strings.TrimSpace(os.Getenv("DEMO_BASE_URL"))
	if base == "" {
		base = "http://localhost:8080"
	}
	return &Client{
		BaseURL: strings.TrimRight(base, "/"),
		HTTP: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func Step(name string) {
	fmt.Printf("\n== %s ==\n", name)
}

func Must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func MustJSON(c *Client, method, path string, body interface{}, headers map[string]string, expectedStatus int, out interface{}) {
	MustJSONOneOf(c, method, path, body, headers, []int{expectedStatus}, out)
}

func MustJSONOneOf(c *Client, method, path string, body interface{}, headers map[string]string, expectedStatuses []int, out interface{}) {
	var payload io.Reader
	if body != nil {
		b := Must(json.Marshal(body))
		payload = bytes.NewBuffer(b)
	}
	req := Must(http.NewRequest(method, c.BaseURL+path, payload))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp := Must(c.HTTP.Do(req))
	defer func() { _ = resp.Body.Close() }()
	raw := Must(io.ReadAll(resp.Body))

	statusOK := false
	for _, allowed := range expectedStatuses {
		if resp.StatusCode == allowed {
			statusOK = true
			break
		}
	}
	if !statusOK {
		panic(fmt.Sprintf("%s %s expected status %v, got %d body=%s", method, path, expectedStatuses, resp.StatusCode, string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			panic(fmt.Sprintf("decode %s %s: %v; body=%s", method, path, err, string(raw)))
		}
	}
}
