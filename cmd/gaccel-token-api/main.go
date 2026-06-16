package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"gaccel-node/internal/tokenapi"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "token-api.yaml", "path to token API config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := tokenapi.LoadConfig(*configPath)
	if err != nil {
		logger.Error("load token api config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := tokenapi.NewServer(cfg, logger)
	if err := server.ListenAndServe(ctx); err != nil {
		logger.Error("token api stopped", "error", err)
		os.Exit(1)
	}
}
