// Package audiobookshelf monitors an Audiobookshelf server: reachability,
// setup state, the API token, libraries, and optionally backup age.
//
// Copy an API token from Audiobookshelf under Settings > Users > your user
// (or Settings > API Keys on newer releases). Without one, Susana only checks
// that the server answers.
package audiobookshelf

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/doctorniz/susana/internal/integration"
	"github.com/doctorniz/susana/internal/integration/apiclient"
)

func init() { integration.Register("audiobookshelf", New) }

// Settings:
//
//	backup_max_age: 48h   warn when the newest backup is older (needs an admin token; off by default)
type Audiobookshelf struct {
	opts         integration.Options
	api          *apiclient.Client
	backupMaxAge time.Duration
}

func New(o integration.Options) (integration.Integration, error) {
	if o.URL == "" {
		return nil, fmt.Errorf("audiobookshelf %q: url is required", o.Name)
	}
	a := &Audiobookshelf{opts: o}
	if v := o.String("backup_max_age", ""); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("audiobookshelf %q: bad backup_max_age: %w", o.Name, err)
		}
		if o.APIKey == "" {
			return nil, fmt.Errorf("audiobookshelf %q: backup_max_age needs an admin api_key", o.Name)
		}
		a.backupMaxAge = d
	}
	headers := map[string]string{}
	if o.APIKey != "" {
		headers["Authorization"] = "Bearer " + o.APIKey
	}
	a.api = apiclient.New(o.URL, headers)
	return a, nil
}

func (a *Audiobookshelf) Name() string { return a.opts.Name }
func (a *Audiobookshelf) Kind() string { return "audiobookshelf" }

type status struct {
	ServerVersion string `json:"serverVersion"`
	IsInit        *bool  `json:"isInit"`
}

type library struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Folders   []struct {
		FullPath string `json:"fullPath"`
	} `json:"folders"`
}

type backup struct {
	CreatedAt int64 `json:"createdAt"` // Unix milliseconds
}

func (a *Audiobookshelf) Check(ctx context.Context) []integration.Result {
	now := time.Now()
	res := func(check string, st integration.Status, summary, detail string) integration.Result {
		return integration.Result{Check: check, Status: st, Summary: summary, Detail: detail, CheckedAt: now}
	}

	var s status
	if err := a.api.Get(ctx, "/status", &s); err != nil {
		return []integration.Result{res("reachable", integration.StatusCritical,
			fmt.Sprintf("%s is not responding", a.opts.Name), err.Error())}
	}
	summary := fmt.Sprintf("%s is responding", a.opts.Name)
	if s.ServerVersion != "" {
		summary += " (Audiobookshelf " + s.ServerVersion + ")"
	}
	out := []integration.Result{res("reachable", integration.StatusOK, summary, "")}
	if s.IsInit != nil && !*s.IsInit {
		return append(out, res("setup", integration.StatusWarning, "Audiobookshelf has not been set up yet", "create the root user in the web UI"))
	}
	if a.opts.APIKey == "" {
		return out
	}

	var libs struct {
		Libraries []library `json:"libraries"`
	}
	if err := a.api.Get(ctx, "/api/libraries", &libs); err != nil {
		return append(out, res("api", integration.StatusCritical, "Audiobookshelf API call failed", err.Error()))
	}
	switch n := len(libs.Libraries); n {
	case 0:
		out = append(out, res("libraries", integration.StatusWarning, "no libraries configured", ""))
	default:
		var names, empty []string
		for _, l := range libs.Libraries {
			names = append(names, fmt.Sprintf("%s (%s)", l.Name, l.MediaType))
			if len(l.Folders) == 0 {
				empty = append(empty, l.Name)
			}
		}
		if len(empty) > 0 {
			out = append(out, res("libraries", integration.StatusWarning,
				fmt.Sprintf("libraries with no folders: %s", strings.Join(empty, ", ")), ""))
		} else {
			out = append(out, res("libraries", integration.StatusOK, fmt.Sprintf("%d libraries", n), strings.Join(names, ", ")))
		}
	}

	if a.backupMaxAge > 0 {
		out = append(out, a.backupResult(ctx, now, res))
	}
	return out
}

func (a *Audiobookshelf) backupResult(ctx context.Context, now time.Time, res func(string, integration.Status, string, string) integration.Result) integration.Result {
	var b struct {
		Backups []backup `json:"backups"`
	}
	if err := a.api.Get(ctx, "/api/backups", &b); err != nil {
		return res("backup", integration.StatusUnknown, "could not read backups (is the token an admin's?)", err.Error())
	}
	var newest int64
	for _, x := range b.Backups {
		newest = max(newest, x.CreatedAt)
	}
	if newest == 0 {
		return res("backup", integration.StatusWarning, "no backups found", "enable scheduled backups under Settings > Backups")
	}
	age := now.Sub(time.UnixMilli(newest))
	summary := fmt.Sprintf("newest backup is %s old", age.Round(time.Minute))
	if age > a.backupMaxAge {
		return res("backup", integration.StatusWarning, summary, fmt.Sprintf("limit is %s", a.backupMaxAge))
	}
	return res("backup", integration.StatusOK, summary, "")
}
