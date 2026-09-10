// Command hyperwake-guest is the Hyperwake guest daemon.
//
// It runs inside every customer machine. It registers once, heartbeats, reports
// activity hints for auto-stop, and shuts the machine down when the control
// plane asks. It listens on no network port, holds no platform credentials and
// knows nothing about other tenants.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/hyperwake/hyperwake-guest/internal/client"
	"github.com/hyperwake/hyperwake-guest/internal/config"
	"github.com/hyperwake/hyperwake-guest/internal/daemon"
	"github.com/hyperwake/hyperwake-guest/internal/sessions"
)

func main() {
	showVersion := flag.Bool("version", false, "print the daemon version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(config.Version)

		return
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := run(log); err != nil {
		log.Error("guest daemon exited", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	store, err := daemon.NewStore(cfg.StateDir)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	d := daemon.New(daemon.Options{
		Config:   cfg,
		Client:   client.New(cfg.Endpoint, cfg.HTTPTimeout),
		Store:    store,
		Activity: sessions.NewReader("/"),
		Logger:   log,
	})

	log.Info("hyperwake-guest starting",
		"version", cfg.Version,
		"endpoint", cfg.Endpoint,
		"identity_source", cfg.Source,
		"platform", sessions.Platform(),
	)

	if err := d.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}
