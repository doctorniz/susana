// Package apiclient is the small JSON-over-HTTP helper the app integrations
// share: GET a path with auth headers and decode the response.
package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls one app's API. Every request carries Headers.
type Client struct {
	Base    string
	Headers map[string]string
	HTTP    *http.Client
}

// New builds a client for base (trailing slashes are trimmed).
func New(base string, headers map[string]string) *Client {
	return &Client{
		Base:    strings.TrimRight(base, "/"),
		Headers: headers,
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

// StatusError is a non-2xx response.
type StatusError struct {
	Path string
	Code int
}

func (e *StatusError) Error() string {
	switch e.Code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Sprintf("API key rejected (HTTP %d)", e.Code)
	}
	return fmt.Sprintf("unexpected HTTP %d from %s", e.Code, e.Path)
}

// IsStatus reports whether err is a StatusError with the given code.
func IsStatus(err error, code int) bool {
	se, ok := err.(*StatusError)
	return ok && se.Code == code
}

// Get fetches path and decodes the JSON body into out. A nil out discards the body.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, 8<<20)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		io.Copy(io.Discard, body)
		p := path
		if i := strings.IndexByte(p, '?'); i >= 0 {
			p = p[:i]
		}
		return &StatusError{Path: p, Code: resp.StatusCode}
	}
	if out == nil {
		_, err = io.Copy(io.Discard, body)
		return err
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
