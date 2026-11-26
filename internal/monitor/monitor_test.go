package monitor

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/doctorniz/susana/internal/integration"
	"github.com/doctorniz/susana/internal/notify"
)

type fakeApp struct{ status integration.Status }

func (f *fakeApp) Name() string { return "app" }
func (f *fakeApp) Kind() string { return "fake" }
func (f *fakeApp) Check(context.Context) []integration.Result {
	return []integration.Result{{Check: "c", Status: f.status, Summary: string(f.status)}}
}

type fakeNotifier struct{ got []notify.Message }

func (f *fakeNotifier) Name() string                  { return "fake" }
func (f *fakeNotifier) MinStatus() integration.Status { return integration.StatusWarning }
func (f *fakeNotifier) Send(_ context.Context, m notify.Message) error {
	f.got = append(f.got, m)
	return nil
}

func TestNotifiesOnChangeOnly(t *testing.T) {
	app := &fakeApp{status: integration.StatusOK}
	n := &fakeNotifier{}
	m := New([]integration.Integration{app}, []notify.Notifier{n}, nil, 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	m.RunOnce(ctx) // healthy start: silent
	m.RunOnce(ctx) // unchanged: silent
	app.status = integration.StatusCritical
	m.RunOnce(ctx) // down: alert
	m.RunOnce(ctx) // still down: silent
	app.status = integration.StatusOK
	m.RunOnce(ctx) // recovered: notice

	if len(n.got) != 2 {
		t.Fatalf("got %d messages, want 2: %+v", len(n.got), n.got)
	}
	if n.got[0].Status != integration.StatusCritical || n.got[1].Status != integration.StatusOK {
		t.Errorf("unexpected messages: %+v", n.got)
	}
}
