package apiclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSendsHeadersAndDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var out struct{ OK bool }
	if err := New(srv.URL+"/", map[string]string{"X-Key": "k"}).Get(context.Background(), "/x", &out); err != nil || !out.OK {
		t.Fatalf("err=%v out=%+v", err, out)
	}
	err := New(srv.URL, nil).Get(context.Background(), "/x?secret=1", &out)
	if !IsStatus(err, http.StatusUnauthorized) {
		t.Fatalf("want 401 StatusError, got %v", err)
	}
	if got := err.Error(); got != "API key rejected (HTTP 401)" {
		t.Errorf("error text = %q", got)
	}
}

func TestStatusErrorHidesQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()
	err := New(srv.URL, nil).Get(context.Background(), "/p?token=abc", nil)
	if err == nil || err.Error() != "unexpected HTTP 418 from /p" {
		t.Fatalf("got %v", err)
	}
}
