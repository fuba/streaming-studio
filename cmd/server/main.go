package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"streaming-studio/internal/api"
	"streaming-studio/internal/app"
	"streaming-studio/internal/model"
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

	stateStore := store.NewFileStoreWithBackup(cfg.StatePath, cfg.StateBackupPath)
	project, err := stateStore.Load()
	if err != nil {
		logger.Fatal(err)
	}
	if err := stateStore.Save(project); err != nil {
		logger.Fatal(err)
	}

	engine := stream.NewEngine(cfg.DataDir, cfg.FFmpegLog, logger)
	textRefresher := stream.NewTextRefresher(stateStore, cfg.DataDir, logger)
	textRefresher.Start()
	defer textRefresher.Stop()
	autoResumeIfNeeded(stateStore, engine, cfg.DataDir, logger)
	stopAutoRecycle := startStreamAutoRecycle(stateStore, engine, cfg.DataDir, 30*time.Minute, time.Minute, logger)
	defer stopAutoRecycle()

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

type streamRunState struct {
	ShouldRun bool      `json:"shouldRun"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type recycleStore interface {
	Load() (model.ProjectState, error)
}

type recycleEngine interface {
	Start(project model.ProjectState) (model.StreamStatus, error)
	Stop() (model.StreamStatus, error)
	Status() model.StreamStatus
}

func startStreamAutoRecycle(stateStore recycleStore, engine recycleEngine, dataDir string, recycleAfter, checkEvery time.Duration, logger *log.Logger) func() {
	if recycleAfter <= 0 {
		return func() {}
	}
	if checkEvery <= 0 {
		checkEvery = time.Minute
	}

	stopCh := make(chan struct{})
	ticker := time.NewTicker(checkEvery)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				status := engine.Status()
				if !status.Running {
					restartDesiredYouTubeStreamIfNeeded(stateStore, engine, dataDir, logger)
					continue
				}
				if status.Mode != model.OutputModeYouTube || status.StartedAt == nil {
					continue
				}
				uptime := time.Since(*status.StartedAt)
				if uptime < recycleAfter {
					continue
				}

				logger.Printf("periodic stream recycle requested mode=%s uptime=%s", status.Mode, uptime)
				if _, err := engine.Stop(); err != nil {
					logger.Printf("periodic stream recycle stop failed: %v", err)
					continue
				}

				deadline := time.Now().Add(10 * time.Second)
				for time.Now().Before(deadline) {
					if !engine.Status().Running {
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
				if engine.Status().Running {
					logger.Printf("periodic stream recycle aborted: stream still running after stop timeout")
					continue
				}

				project, err := stateStore.Load()
				if err != nil {
					logger.Printf("periodic stream recycle load state failed: %v", err)
					continue
				}
				if _, err := engine.Start(project); err != nil {
					logger.Printf("periodic stream recycle start failed: %v", err)
					continue
				}
				logger.Printf("periodic stream recycle completed")
			case <-stopCh:
				return
			}
		}
	}()

	return func() {
		close(stopCh)
	}
}

func restartDesiredYouTubeStreamIfNeeded(stateStore recycleStore, engine recycleEngine, dataDir string, logger *log.Logger) {
	runState, err := loadStreamRunState(dataDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Printf("failed to read stream run state for restart: %v", err)
		}
		return
	}
	if !runState.ShouldRun {
		return
	}

	project, err := stateStore.Load()
	if err != nil {
		logger.Printf("desired stream restart load state failed: %v", err)
		return
	}
	if project.Output.Mode != model.OutputModeYouTube {
		return
	}

	if _, err := engine.Start(project); err != nil {
		logger.Printf("desired youtube stream restart failed: %v", err)
		return
	}
	logger.Printf("desired youtube stream restarted")
}

func autoResumeIfNeeded(stateStore *store.FileStore, engine *stream.Engine, dataDir string, logger *log.Logger) {
	runState, err := loadStreamRunState(dataDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Printf("failed to read stream run state: %v", err)
		}
		return
	}
	if !runState.ShouldRun {
		return
	}

	project, err := stateStore.Load()
	if err != nil {
		logger.Printf("failed to load project state for auto-resume: %v", err)
		return
	}

	if _, err := engine.Start(project); err != nil {
		logger.Printf("auto-resume start failed: %v", err)
		return
	}
	logger.Printf("auto-resume stream started after process restart")
}

func loadStreamRunState(dataDir string) (streamRunState, error) {
	statePath := filepath.Join(dataDir, "runtime", "stream-run-state.json")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		return streamRunState{}, err
	}

	var runState streamRunState
	if err := json.Unmarshal(raw, &runState); err != nil {
		return streamRunState{}, err
	}
	return runState, nil
}
