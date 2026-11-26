// Package notify sends alerts to chat channels. v1 channels are send-only;
// two-way chat (questions and approvals) comes later.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/doctorniz/susana/internal/config"
	"github.com/doctorniz/susana/internal/integration"
)

// Message is one alert or recovery notice.
type Message struct {
	Title  string
	Body   string
	Status integration.Status
}

// Notifier delivers messages to one channel.
type Notifier interface {
	Name() string
	MinStatus() integration.Status
	Send(ctx context.Context, m Message) error
}

var client = &http.Client{Timeout: 10 * time.Second}

// New builds a notifier from config.
func New(c config.Notifier) (Notifier, error) {
	min := integration.StatusWarning
	if c.MinLevel == "critical" {
		min = integration.StatusCritical
	}
	switch c.Kind {
	case "discord":
		if c.URL == "" {
			return nil, fmt.Errorf("discord notifier needs url (a webhook URL)")
		}
		return &discord{url: c.URL, min: min}, nil
	case "telegram":
		if c.Token == "" || c.ChatID == "" {
			return nil, fmt.Errorf("telegram notifier needs token and chat_id")
		}
		return &telegram{token: c.Token, chatID: c.ChatID, min: min}, nil
	case "ntfy":
		if c.URL == "" {
			return nil, fmt.Errorf("ntfy notifier needs url (the topic URL)")
		}
		return &ntfy{url: c.URL, token: c.Token, min: min}, nil
	}
	return nil, fmt.Errorf("unknown notifier kind %q (want discord, telegram or ntfy)", c.Kind)
}

func icon(s integration.Status) string {
	switch s {
	case integration.StatusOK:
		return "✅"
	case integration.StatusCritical:
		return "🔴"
	}
	return "🟠"
}

func post(ctx context.Context, u, contentType string, body []byte, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

type discord struct {
	url string
	min integration.Status
}

func (d *discord) Name() string                  { return "discord" }
func (d *discord) MinStatus() integration.Status { return d.min }
func (d *discord) Send(ctx context.Context, m Message) error {
	b, _ := json.Marshal(map[string]string{"content": fmt.Sprintf("%s **%s**\n%s", icon(m.Status), m.Title, m.Body)})
	return post(ctx, d.url, "application/json", b, nil)
}

type telegram struct {
	token, chatID string
	min           integration.Status
}

func (t *telegram) Name() string                  { return "telegram" }
func (t *telegram) MinStatus() integration.Status { return t.min }
func (t *telegram) Send(ctx context.Context, m Message) error {
	b, _ := json.Marshal(map[string]string{
		"chat_id": t.chatID,
		"text":    fmt.Sprintf("%s %s\n%s", icon(m.Status), m.Title, m.Body),
	})
	return post(ctx, "https://api.telegram.org/bot"+url.PathEscape(t.token)+"/sendMessage", "application/json", b, nil)
}

type ntfy struct {
	url, token string
	min        integration.Status
}

func (n *ntfy) Name() string                  { return "ntfy" }
func (n *ntfy) MinStatus() integration.Status { return n.min }
func (n *ntfy) Send(ctx context.Context, m Message) error {
	h := map[string]string{"Title": strings.ReplaceAll(m.Title, "\n", " ")}
	if m.Status == integration.StatusCritical {
		h["Priority"] = "high"
	}
	if n.token != "" {
		h["Authorization"] = "Bearer " + n.token
	}
	return post(ctx, n.url, "text/plain", []byte(m.Body), h)
}
