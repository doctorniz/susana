package httpcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/doctorniz/susana/internal/integration"
)

func TestHTTPCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte(`{"data":{"status":"ok"}}`))
		case "/degraded":
			w.Write([]byte(`{"data":{"status":"degraded"}}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	cases := []struct {
		path string
		want integration.Status
	}{
		{"/ok", integration.StatusOK},
		{"/degraded", integration.StatusCritical},
		{"/broken", integration.StatusCritical},
	}
	for _, c := range cases {
		h, err := New(integration.Options{Name: "app", Kind: "http", URL: srv.URL + c.path,
			Settings: map[string]any{"json_field": "data.status", "json_equals": "ok"}})
		if err != nil {
			t.Fatal(err)
		}
		if got := h.Check(context.Background())[0].Status; got != c.want {
			t.Errorf("%s: status %s, want %s", c.path, got, c.want)
		}
	}
}
