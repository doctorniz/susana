package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/doctorniz/susana/internal/config"
)

func TestOpenAICompatible(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		if in["model"] != "test-model" {
			t.Errorf("model %v", in["model"])
		}
		w.Write([]byte(`{"choices":[{"message":{"content":" Disk is full. "}}]}`))
	}))
	defer srv.Close()

	p, err := New(&config.LLM{Provider: "openai", BaseURL: srv.URL + "/v1", Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Complete(context.Background(), "sys", "user")
	if err != nil || got != "Disk is full." {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestNoProviderIsFine(t *testing.T) {
	p, err := New(nil)
	if p != nil || err != nil {
		t.Fatalf("want nil, nil; got %v, %v", p, err)
	}
}
