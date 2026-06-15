package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"gaccel-node/internal/admin"
	"gaccel-node/internal/config"
	"gaccel-node/internal/metrics"
	"gaccel-node/internal/quicserver"
	"gaccel-node/internal/sessions"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfgManager, err := config.NewManager(*configPath)
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	collector := metrics.NewCollector()
	sessionRegistry := sessions.NewRegistry()

	adminServer := admin.NewServer(cfgManager, logger, collector, sessionRegistry)
	go func() {
		if err := adminServer.ListenAndServe(ctx); err != nil {
			logger.Error("admin server stopped", "error", err)
			stop()
		}
	}()

	server, err := quicserver.New(cfgManager, logger, collector, sessionRegistry)
	if err != nil {
		logger.Error("create quic server", "error", err)
		os.Exit(1)
	}

	if err := server.ListenAndServe(ctx); err != nil {
		logger.Error("quic server stopped", "error", err)
		os.Exit(1)
	}
}
