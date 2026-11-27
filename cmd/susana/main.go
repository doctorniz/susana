// Command susana is a self-hosted, read-only monitoring agent for homelabs.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/doctorniz/susana/internal/config"
	"github.com/doctorniz/susana/internal/integration"
	"github.com/doctorniz/susana/internal/llm"
	"github.com/doctorniz/susana/internal/mcpserver"
	"github.com/doctorniz/susana/internal/monitor"
	"github.com/doctorniz/susana/internal/notify"
	"github.com/doctorniz/susana/internal/server"

	// Integrations register themselves on import.
	_ "github.com/doctorniz/susana/internal/integration/arr"
	_ "github.com/doctorniz/susana/internal/integration/docker"
	_ "github.com/doctorniz/susana/internal/integration/host"
	_ "github.com/doctorniz/susana/internal/integration/httpcheck"
	_ "github.com/doctorniz/susana/internal/integration/proxmox"
)

var version = "dev"

func main() {
	cfgPath := flag.String("config", "/config/susana.yaml", "path to the config file")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("susana", version)
		return
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(*cfgPath, log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(cfgPath string, log *slog.Logger) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	var apps []integration.Integration
	for _, c := range cfg.Integrations {
		app, err := integration.New(integration.Options{
			Name: c.Name, Kind: c.Kind, URL: c.URL, APIKey: c.APIKey, Settings: c.Settings,
		})
		if err != nil {
			return err
		}
		apps = append(apps, app)
	}
	var notifiers []notify.Notifier
	for _, c := range cfg.Notifiers {
		n, err := notify.New(c)
		if err != nil {
			return err
		}
		notifiers = append(notifiers, n)
	}
	provider, err := llm.New(cfg.LLM)
	if err != nil {
		return err
	}

	log.Info("starting susana", "version", version, "integrations", len(apps), "notifiers", len(notifiers),
		"llm", provider != nil, "listen", cfg.Listen)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mon := monitor.New(apps, notifiers, provider, cfg.Interval, log)
	go mon.Run(ctx)

	if cfg.APIToken == "" {
		log.Warn("api_token is not set: /api and /mcp are open to anyone who can reach this port")
	}
	mcpSrv := mcpserver.Handler(mcpserver.New(mon, version))
	srv := &http.Server{Addr: cfg.Listen, Handler: server.New(mon, mcpSrv, cfg.APIToken), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shut)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
