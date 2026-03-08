package main

import (
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"streaming-studio/internal/api"
	"streaming-studio/internal/app"
	"streaming-studio/internal/store"
	"streaming-studio/internal/stream"
)

func main() {
	cfg := app.LoadConfig()
	logger, closeLogger, err := app.NewLogger(cfg.LogPath)
	if err != nil {
		panic(err)
	}
	defer closeLogger()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		logger.Fatal(err)
	}

	stateStore := store.NewFileStore(cfg.StatePath)
	if _, err := stateStore.Load(); err != nil {
		logger.Fatal(err)
	}

	engine := stream.NewEngine(cfg.DataDir, cfg.FFmpegLog, logger)
	textRefresher := stream.NewTextRefresher(stateStore, cfg.DataDir, logger)
	textRefresher.Start()
	defer textRefresher.Stop()

	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: api.NewServer(stateStore, engine, cfg.DataDir, cfg.UIDistDir, logger).Handler(),
	}

	go func() {
		logger.Printf("http server listening on %s", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal(err)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals

	if _, err := engine.Stop(); err != nil {
		logger.Printf("failed to stop engine: %v", err)
	}
	if err := server.Close(); err != nil {
		logger.Printf("failed to close server: %v", err)
	}
}
