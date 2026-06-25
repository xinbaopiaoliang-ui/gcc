package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"gaccel-node/internal/panel"

	"golang.org/x/crypto/bcrypt"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "panel.yaml", "path to panel config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	createAdmin := flag.Bool("create-admin", false, "create or update a panel admin user and exit")
	adminUsername := flag.String("admin-username", "admin", "panel admin username for -create-admin")
	adminPassword := flag.String("admin-password", "", "panel admin password for -create-admin")
	adminRole := flag.String("admin-role", panel.PanelUserRoleAdmin, "panel user role for -create-admin")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := panel.LoadConfig(*configPath)
	if err != nil {
		logger.Error("load panel config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := panel.OpenMySQLStore(ctx, cfg.Database.DSN)
	if err != nil {
		logger.Error("open panel database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	if *createAdmin {
		username := strings.TrimSpace(*adminUsername)
		password := strings.TrimSpace(*adminPassword)
		if username == "" || password == "" {
			logger.Error("create admin", "error", "admin username and password are required")
			os.Exit(1)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			logger.Error("hash admin password", "error", err)
			os.Exit(1)
		}
		user, err := store.UpsertPanelUser(ctx, username, string(hash), strings.TrimSpace(*adminRole), panel.PanelUserStatusActive)
		if err != nil {
			logger.Error("upsert panel admin", "error", err)
			os.Exit(1)
		}
		fmt.Printf("panel user ready: %s role=%s status=%s\n", user.Username, user.Role, user.Status)
		return
	}

	server := panel.NewServer(cfg, logger, version, store)
	if err := server.ListenAndServe(ctx); err != nil {
		logger.Error("panel stopped", "error", err)
		os.Exit(1)
	}
}
