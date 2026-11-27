// Package jellyfin monitors a Jellyfin media server: reachability, server
// state (pending restart), failed scheduled tasks and active streams.
//
// Create an API key in Jellyfin under Dashboard > API Keys. Without one,
// Susana only checks that the server answers.
package jellyfin

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/doctorniz/susana/internal/integration"
	"github.com/doctorniz/susana/internal/integration/apiclient"
)

func init() { integration.Register("jellyfin", New) }

// Settings:
//
//	ignore_tasks: [Scan Media Library]   scheduled tasks never to alert on
type Jellyfin struct {
	opts        integration.Options
	api         *apiclient.Client
	ignoreTasks []string
}

func New(o integration.Options) (integration.Integration, error) {
	if o.URL == "" {
		return nil, fmt.Errorf("jellyfin %q: url is required", o.Name)
	}
	headers := map[string]string{}
	if o.APIKey != "" {
		// The MediaBrowser scheme works on every 10.x release; X-Emby-Token is legacy.
		headers["Authorization"] = fmt.Sprintf(`MediaBrowser Client="Susana", Token="%s"`, o.APIKey)
	}
	return &Jellyfin{opts: o, api: apiclient.New(o.URL, headers), ignoreTasks: o.StringList("ignore_tasks")}, nil
}

func (j *Jellyfin) Name() string { return j.opts.Name }
func (j *Jellyfin) Kind() string { return "jellyfin" }

type publicInfo struct {
	ServerName string `json:"ServerName"`
	Version    string `json:"Version"`
}

type systemInfo struct {
	Version            string `json:"Version"`
	HasPendingRestart  bool   `json:"HasPendingRestart"`
	HasUpdateAvailable bool   `json:"HasUpdateAvailable"`
}

type task struct {
	Name       string `json:"Name"`
	State      string `json:"State"`
	IsHidden   bool   `json:"IsHidden"`
	LastResult *struct {
		Status       string    `json:"Status"` // Completed, Failed, Cancelled, Aborted
		ErrorMessage string    `json:"ErrorMessage"`
		EndTimeUtc   time.Time `json:"EndTimeUtc"`
	} `json:"LastExecutionResult"`
}

type session struct {
	UserName       string `json:"UserName"`
	NowPlayingItem *struct {
		Name string `json:"Name"`
	} `json:"NowPlayingItem"`
	PlayState struct {
		IsPaused   bool   `json:"IsPaused"`
		PlayMethod string `json:"PlayMethod"` // DirectPlay, DirectStream, Transcode
	} `json:"PlayState"`
}

func (j *Jellyfin) Check(ctx context.Context) []integration.Result {
	now := time.Now()
	res := func(check string, st integration.Status, summary, detail string) integration.Result {
		return integration.Result{Check: check, Status: st, Summary: summary, Detail: detail, CheckedAt: now}
	}

	var pub publicInfo
	if err := j.api.Get(ctx, "/System/Info/Public", &pub); err != nil {
		return []integration.Result{res("reachable", integration.StatusCritical,
			fmt.Sprintf("%s is not responding", j.opts.Name), err.Error())}
	}
	out := []integration.Result{res("reachable", integration.StatusOK,
		fmt.Sprintf("%s is responding (Jellyfin %s)", j.opts.Name, pub.Version), "")}
	if j.opts.APIKey == "" {
		return out
	}

	var info systemInfo
	if err := j.api.Get(ctx, "/System/Info", &info); err != nil {
		return append(out, res("api", integration.StatusCritical, "Jellyfin API call failed", err.Error()))
	}
	switch {
	case info.HasPendingRestart:
		out = append(out, res("server", integration.StatusWarning, "Jellyfin needs a restart to apply changes", ""))
	case info.HasUpdateAvailable:
		out = append(out, res("server", integration.StatusOK, "server running; an update is available", ""))
	default:
		out = append(out, res("server", integration.StatusOK, "server running", ""))
	}

	var tasks []task
	if err := j.api.Get(ctx, "/ScheduledTasks?isHidden=false", &tasks); err != nil {
		out = append(out, res("tasks", integration.StatusUnknown, "could not read scheduled tasks", err.Error()))
	} else {
		out = append(out, j.taskResults(tasks, res)...)
	}

	var sessions []session
	if err := j.api.Get(ctx, "/Sessions?activeWithinSeconds=960", &sessions); err != nil {
		out = append(out, res("streams", integration.StatusUnknown, "could not read sessions", err.Error()))
	} else {
		out = append(out, streamsResult(sessions, res))
	}
	return out
}

type mkResult func(check string, st integration.Status, summary, detail string) integration.Result

func (j *Jellyfin) taskResults(tasks []task, res mkResult) []integration.Result {
	var out []integration.Result
	for _, t := range tasks {
		if t.IsHidden || t.LastResult == nil || slices.Contains(j.ignoreTasks, t.Name) {
			continue
		}
		if st := t.LastResult.Status; st == "Failed" || st == "Aborted" {
			out = append(out, res("task:"+t.Name, integration.StatusWarning,
				fmt.Sprintf("scheduled task %q %s at %s", t.Name, strings.ToLower(st), t.LastResult.EndTimeUtc.Format(time.RFC3339)),
				t.LastResult.ErrorMessage))
		}
	}
	if len(out) == 0 {
		out = append(out, res("tasks", integration.StatusOK, "no failed scheduled tasks", ""))
	}
	return out
}

func streamsResult(sessions []session, res mkResult) integration.Result {
	var playing, transcoding int
	var who []string
	for _, s := range sessions {
		if s.NowPlayingItem == nil {
			continue
		}
		playing++
		if s.PlayState.PlayMethod == "Transcode" {
			transcoding++
		}
		who = append(who, fmt.Sprintf("%s: %s", s.UserName, s.NowPlayingItem.Name))
	}
	summary := fmt.Sprintf("%d active streams (%d transcoding)", playing, transcoding)
	return res("streams", integration.StatusOK, summary, strings.Join(who, "; "))
}
