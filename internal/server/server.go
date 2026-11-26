// Package server serves the status page and JSON API.
package server

import (
	"encoding/json"
	"html/template"
	"net/http"

	"github.com/doctorniz/susana/internal/monitor"
)

var page = template.Must(template.New("status").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="refresh" content="30"><title>Susanah</title>
<style>
body{font-family:system-ui,sans-serif;max-width:860px;margin:2rem auto;padding:0 1rem;background:#fafafa;color:#222}
@media (prefers-color-scheme:dark){body{background:#151515;color:#eee}td{border-color:#333!important}}
table{width:100%;border-collapse:collapse}td{padding:.5rem;border-bottom:1px solid #ddd;vertical-align:top}
.ok{color:#2e7d32}.warning{color:#ef6c00}.critical{color:#c62828}.unknown{color:#777}small{opacity:.7}
</style></head><body>
<h1>Susanah</h1>
{{if not .}}<p>No integrations configured yet. Add some to susanah.yaml.</p>{{end}}
<table>{{range .}}<tr><td><strong>{{.Name}}</strong><br><small>{{.Kind}}</small></td>
<td class="{{.Status}}">{{.Status}}</td><td>{{range .Results}}<div class="{{.Status}}">{{.Summary}}</div>{{end}}</td></tr>{{end}}</table>
</body></html>`))

// New returns the HTTP handler.
func New(m *monitor.Monitor) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(m.Snapshot())
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page.Execute(w, m.Snapshot())
	})
	return mux
}
