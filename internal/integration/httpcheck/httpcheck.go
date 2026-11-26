// Package httpcheck is the generic integration for any app with an HTTP
// endpoint, including your own self-built apps. Configure what "healthy"
// looks like and Susanah checks it.
package httpcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/doctorniz/susana/internal/integration"
)

func init() { integration.Register("http", New) }

// Check settings (all optional except the instance URL):
//
//	expect_status:  200          HTTP status that counts as healthy
//	body_contains:  "Healthy"    substring the body must contain
//	json_field:     status       dot path into a JSON body, e.g. "data.status"
//	json_equals:    ok           value json_field must equal
//	headers:        {Authorization: "Bearer ..."}
//	timeout:        10s
type HTTP struct {
	opts         integration.Options
	client       *http.Client
	expectStatus int
	bodyContains string
	jsonField    string
	jsonEquals   string
	headers      map[string]string
}

// New builds a generic HTTP check.
func New(o integration.Options) (integration.Integration, error) {
	if o.URL == "" {
		return nil, fmt.Errorf("http %q: url is required", o.Name)
	}
	h := &HTTP{opts: o, expectStatus: http.StatusOK, headers: map[string]string{}}
	timeout := 10 * time.Second
	s := o.Settings
	if v, ok := s["expect_status"].(int); ok {
		h.expectStatus = v
	}
	h.bodyContains, _ = s["body_contains"].(string)
	h.jsonField, _ = s["json_field"].(string)
	if v, ok := s["json_equals"]; ok {
		h.jsonEquals = fmt.Sprint(v)
	}
	if v, ok := s["timeout"].(string); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("http %q: bad timeout: %w", o.Name, err)
		}
		timeout = d
	}
	if hm, ok := s["headers"].(map[string]any); ok {
		for k, v := range hm {
			h.headers[k] = fmt.Sprint(v)
		}
	}
	if o.APIKey != "" {
		if _, set := h.headers["Authorization"]; !set {
			h.headers["Authorization"] = "Bearer " + o.APIKey
		}
	}
	h.client = &http.Client{Timeout: timeout}
	return h, nil
}

func (h *HTTP) Name() string { return h.opts.Name }
func (h *HTTP) Kind() string { return "http" }

// Check fetches the URL and compares the response to the expectations.
func (h *HTTP) Check(ctx context.Context) []integration.Result {
	now := time.Now()
	fail := func(summary, detail string) []integration.Result {
		return []integration.Result{{Check: "http", Status: integration.StatusCritical, Summary: summary, Detail: detail, CheckedAt: now}}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.opts.URL, nil)
	if err != nil {
		return fail("bad request", err.Error())
	}
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := h.client.Do(req)
	if err != nil {
		return fail(fmt.Sprintf("%s is not responding", h.opts.Name), err.Error())
	}
	defer resp.Body.Close()
	latency := time.Since(start).Round(time.Millisecond)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != h.expectStatus {
		return fail(fmt.Sprintf("%s returned HTTP %d, expected %d", h.opts.Name, resp.StatusCode, h.expectStatus), "")
	}
	if h.bodyContains != "" && !strings.Contains(string(body), h.bodyContains) {
		return fail(fmt.Sprintf("%s response does not contain %q", h.opts.Name, h.bodyContains), "")
	}
	if h.jsonField != "" {
		got, err := lookup(body, h.jsonField)
		if err != nil {
			return fail(fmt.Sprintf("%s: %v", h.opts.Name, err), "")
		}
		if h.jsonEquals != "" && got != h.jsonEquals {
			return fail(fmt.Sprintf("%s: %s is %q, expected %q", h.opts.Name, h.jsonField, got, h.jsonEquals), "")
		}
	}
	return []integration.Result{{
		Check: "http", Status: integration.StatusOK,
		Summary: fmt.Sprintf("%s is healthy (%s)", h.opts.Name, latency), CheckedAt: now,
	}}
}

// lookup walks a dot path such as "data.status" through a JSON object.
func lookup(body []byte, path string) (string, error) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("response is not JSON")
	}
	for _, part := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return "", fmt.Errorf("field %q not found", path)
		}
		if v, ok = m[part]; !ok {
			return "", fmt.Errorf("field %q not found", path)
		}
	}
	return fmt.Sprint(v), nil
}
