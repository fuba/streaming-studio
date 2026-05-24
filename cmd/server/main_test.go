package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"streaming-studio/internal/model"
)

type fakeRecycleStore struct {
	project model.ProjectState
	err     error
}

func (f *fakeRecycleStore) Load() (model.ProjectState, error) {
	return f.project, f.err
}

type fakeRecycleEngine struct {
	mu        sync.Mutex
	status    model.StreamStatus
	startErr  error
	stopErr   error
	failStart int
	startCall int
	stopCall  int
}

func (f *fakeRecycleEngine) Start(project model.ProjectState) (model.StreamStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCall++
	if f.failStart > 0 {
		f.failStart--
		return f.status, errors.New("planned start failure")
	}
	if f.startErr != nil {
		return f.status, f.startErr
	}
	startedAt := time.Now().UTC()
	f.status.Running = true
	f.status.Mode = project.Output.Mode
	f.status.StartedAt = &startedAt
	return f.status, nil
}

func (f *fakeRecycleEngine) Stop() (model.StreamStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCall++
	if f.stopErr != nil {
		return f.status, f.stopErr
	}
	f.status.Running = false
	return f.status, nil
}

func (f *fakeRecycleEngine) Status() model.StreamStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func TestStartStreamAutoRecycleRestartsLongRunningYouTubeStream(t *testing.T) {
	startedAt := time.Now().Add(-2 * time.Hour).UTC()
	engine := &fakeRecycleEngine{
		status: model.StreamStatus{
			Running:   true,
			Mode:      model.OutputModeYouTube,
			StartedAt: &startedAt,
		},
	}
	store := &fakeRecycleStore{
		project: model.DefaultProjectState(),
	}
	store.project.Output.Mode = model.OutputModeYouTube
	logger := log.New(bytes.NewBuffer(nil), "", 0)
	dataDir := t.TempDir()

	stop := startStreamAutoRecycle(store, engine, dataDir, 200*time.Millisecond, 20*time.Millisecond, logger)
	defer stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		engine.mu.Lock()
		startCalls := engine.startCall
		stopCalls := engine.stopCall
		engine.mu.Unlock()
		if startCalls >= 1 && stopCalls >= 1 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	engine.mu.Lock()
	defer engine.mu.Unlock()
	t.Fatalf("recycle not triggered: startCalls=%d stopCalls=%d", engine.startCall, engine.stopCall)
}

func TestStartStreamAutoRecycleSkipsNonYouTubeStream(t *testing.T) {
	startedAt := time.Now().Add(-2 * time.Hour).UTC()
	engine := &fakeRecycleEngine{
		status: model.StreamStatus{
			Running:   true,
			Mode:      model.OutputModeHLS,
			StartedAt: &startedAt,
		},
	}
	store := &fakeRecycleStore{
		project: model.DefaultProjectState(),
	}
	store.project.Output.Mode = model.OutputModeHLS
	logger := log.New(bytes.NewBuffer(nil), "", 0)
	dataDir := t.TempDir()

	stop := startStreamAutoRecycle(store, engine, dataDir, 200*time.Millisecond, 20*time.Millisecond, logger)
	defer stop()
	time.Sleep(300 * time.Millisecond)

	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.startCall != 0 || engine.stopCall != 0 {
		t.Fatalf("unexpected recycle for non-youtube: startCalls=%d stopCalls=%d", engine.startCall, engine.stopCall)
	}
}

func TestStartStreamAutoRecycleRetriesDesiredYouTubeStreamAfterStartFailure(t *testing.T) {
	engine := &fakeRecycleEngine{
		failStart: 1,
		status: model.StreamStatus{
			Running: false,
			Mode:    model.OutputModeYouTube,
		},
	}
	store := &fakeRecycleStore{
		project: model.DefaultProjectState(),
	}
	store.project.Output.Mode = model.OutputModeYouTube
	logger := log.New(bytes.NewBuffer(nil), "", 0)
	dataDir := t.TempDir()
	writeTestRunState(t, dataDir, true)

	stop := startStreamAutoRecycle(store, engine, dataDir, 200*time.Millisecond, 20*time.Millisecond, logger)
	defer stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		engine.mu.Lock()
		startCalls := engine.startCall
		running := engine.status.Running
		engine.mu.Unlock()
		if startCalls >= 2 && running {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	engine.mu.Lock()
	defer engine.mu.Unlock()
	t.Fatalf("desired stream was not retried: startCalls=%d running=%v", engine.startCall, engine.status.Running)
}

func writeTestRunState(t *testing.T, dataDir string, shouldRun bool) {
	t.Helper()

	runtimeDir := filepath.Join(dataDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() returned error: %v", err)
	}
	payload, err := json.Marshal(streamRunState{ShouldRun: shouldRun, UpdatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "stream-run-state.json"), payload, 0o644); err != nil {
		t.Fatalf("os.WriteFile() returned error: %v", err)
	}
}
