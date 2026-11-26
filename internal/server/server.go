// Package server serves the status page and JSON API.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"

	"github.com/doctorniz/susana/internal/monitor"
)

var page = template.Must(template.New("status").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="refresh" content="30"><title>Susana</title>
<style>
body{font-family:system-ui,sans-serif;max-width:860px;margin:2rem auto;padding:0 1rem;background:#fafafa;color:#222}
@media (prefers-color-scheme:dark){body{background:#151515;color:#eee}td{border-color:#333!important}}
table{width:100%;border-collapse:collapse}td{padding:.5rem;border-bottom:1px solid #ddd;vertical-align:top}
.ok{color:#2e7d32}.warning{color:#ef6c00}.critical{color:#c62828}.unknown{color:#777}small{opacity:.7}
</style></head><body>
<h1>Susana</h1>
{{if not .}}<p>No integrations configured yet. Add some to susana.yaml.</p>{{end}}
<table>{{range .}}<tr><td><strong>{{.Name}}</strong><br><small>{{.Kind}}</small></td>
<td class="{{.Status}}">{{.Status}}</td><td>{{range .Results}}<div class="{{.Status}}">{{.Summary}}</div>{{end}}</td></tr>{{end}}</table>
</body></html>`))

// New returns the HTTP handler. When token is set, /api and /mcp require
// "Authorization: Bearer <token>"; the status page and /healthz stay open.
func New(m *monitor.Monitor, mcpHandler http.Handler, token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page.Execute(w, m.Snapshot())
	})

	api := http.NewServeMux()
	api.HandleFunc("GET /api/summary", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, m.Summarize())
	})
	api.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, m.Snapshot())
	})
	api.HandleFunc("GET /api/apps/{name}", func(w http.ResponseWriter, r *http.Request) {
		app, ok := m.App(r.PathValue("name"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such app"})
			return
		}
		writeJSON(w, http.StatusOK, app)
	})
	api.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 {
			limit = 50
		}
		writeJSON(w, http.StatusOK, m.Events(r.URL.Query().Get("app"), limit))
	})
	mux.Handle("/api/", requireToken(token, api))
	if mcpHandler != nil {
		mux.Handle("/mcp", requireToken(token, mcpHandler))
	}
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func requireToken(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="susana"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or wrong bearer token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
