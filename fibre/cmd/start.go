package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/celestiaorg/celestia-app/v8/fibre"
)

// initServerConfig creates the home directory and writes a default config file
// if one does not already exist.
func initServerConfig(home string) error {
	configPath := fibre.DefaultConfigPath(home)
	_, err := os.Stat(configPath)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking config file %s: %w", configPath, err)
	}

	defCfg := fibre.DefaultServerConfig()
	if err := defCfg.Save(configPath); err != nil {
		return err
	}

	slog.Info("initialized fibre home dir", "path", home)
	return nil
}

// startServer creates, starts, and runs the fibre server until the context
// is cancelled, then gracefully shuts it down.
func startServer(ctx context.Context, cfg fibre.ServerConfig) error {
	server, err := fibre.NewServer(cfg)
	if err != nil {
		return fmt.Errorf("creating server: %w", err)
	}

	startCtx, startCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer startCancel()

	if err := server.Start(startCtx); err != nil {
		return fmt.Errorf("starting server: %w", err)
	}

	server.Config.Log.Info("server started",
		"listen", server.ListenAddress(),
		"app_grpc", cfg.AppGRPCAddress,
		"chain_id", server.ChainID(),
		"store", cfg.Path,
	)

	<-startCtx.Done()
	startCancel()

	stopCtx, stopCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stopCancel()

	if err := server.Stop(stopCtx); err != nil {
		return fmt.Errorf("stopping server: %w", err)
	}

	server.Config.Log.Info("server stopped")
	return nil
}
