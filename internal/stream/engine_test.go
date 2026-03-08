package stream

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"streaming-studio/internal/model"
)

func TestEngineRestartsAfterUnexpectedExit(t *testing.T) {
	t.Parallel()

	engine := NewEngine(t.TempDir(), t.TempDir()+"/ffmpeg.log", log.New(bytes.NewBuffer(nil), "", 0))
	engine.restartDelay = 10 * time.Millisecond
	engine.commandFactory = helperCommandFactory("exit-1", "hold")

	project := testStreamProject()
	status, err := engine.Start(project)
	if err != nil {
		t.Fatalf("engine.Start() returned error: %v", err)
	}

	waitForEngineState(t, engine, func(current model.StreamStatus) bool {
		return current.Running && current.PID != 0 && current.PID != status.PID
	})

	if _, err := engine.Stop(); err != nil {
		t.Fatalf("engine.Stop() returned error: %v", err)
	}
	waitForEngineState(t, engine, func(current model.StreamStatus) bool {
		return !current.Running
	})
}

func TestEngineDoesNotRestartAfterManualStop(t *testing.T) {
	t.Parallel()

	engine := NewEngine(t.TempDir(), t.TempDir()+"/ffmpeg.log", log.New(bytes.NewBuffer(nil), "", 0))
	engine.restartDelay = 10 * time.Millisecond
	engine.commandFactory = helperCommandFactory("hold", "hold")

	project := testStreamProject()
	if _, err := engine.Start(project); err != nil {
		t.Fatalf("engine.Start() returned error: %v", err)
	}

	if _, err := engine.Stop(); err != nil {
		t.Fatalf("engine.Stop() returned error: %v", err)
	}
	waitForEngineState(t, engine, func(current model.StreamStatus) bool {
		return !current.Running
	})

	time.Sleep(30 * time.Millisecond)

	status := engine.Status()
	if status.Running {
		t.Fatalf("engine restarted after manual stop: %+v", status)
	}
}

func TestEngineRestartUsesUpdatedDesiredProject(t *testing.T) {
	t.Parallel()

	engine := NewEngine(t.TempDir(), t.TempDir()+"/ffmpeg.log", log.New(bytes.NewBuffer(nil), "", 0))
	engine.restartDelay = 50 * time.Millisecond
	engine.commandFactory = helperCommandFactory("exit-1", "hold")

	initialProject := testStreamProject()
	initialProject.Output.YouTube.StreamKey = "initial-stream-key"
	initialStatus, err := engine.Start(initialProject)
	if err != nil {
		t.Fatalf("engine.Start() returned error: %v", err)
	}

	waitForEngineState(t, engine, func(current model.StreamStatus) bool {
		return !current.Running && current.LastError != ""
	})

	updatedProject := testStreamProject()
	updatedProject.Output.YouTube.StreamKey = "updated-stream-key"
	engine.UpdateProject(updatedProject)

	waitForEngineState(t, engine, func(current model.StreamStatus) bool {
		return current.Running && current.PID != 0 && current.PID != initialStatus.PID
	})

	status := engine.Status()
	command := strings.Join(status.Command, " ")
	if !strings.Contains(command, "updated-stream-key") {
		t.Fatalf("command = %q, want updated stream key", command)
	}

	if _, err := engine.Stop(); err != nil {
		t.Fatalf("engine.Stop() returned error: %v", err)
	}
	waitForEngineState(t, engine, func(current model.StreamStatus) bool {
		return !current.Running
	})
}

func TestEngineHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_ENGINE_HELPER") != "1" {
		return
	}

	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "exit-1":
		os.Exit(1)
	case "hold":
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

func helperCommandFactory(modes ...string) func([]string) *exec.Cmd {
	var mu sync.Mutex
	index := 0

	return func(args []string) *exec.Cmd {
		mu.Lock()
		mode := modes[index]
		if index < len(modes)-1 {
			index++
		}
		mu.Unlock()

		cmd := exec.Command(os.Args[0], "-test.run=TestEngineHelperProcess", "--", mode)
		cmd.Env = append(os.Environ(), "GO_WANT_ENGINE_HELPER=1")
		return cmd
	}
}

func testStreamProject() model.ProjectState {
	project := model.DefaultProjectState()
	project.Output.Mode = model.OutputModeYouTube
	project.Output.YouTube.StreamKey = "abcd-efgh-ijkl"
	project.Sources = []model.Source{
		{
			ID:      "cam-a",
			Name:    "Camera A",
			Kind:    model.SourceKindHLS,
			Enabled: true,
			Layout: model.Layout{
				Width:   1280,
				Height:  720,
				Opacity: 1,
			},
			HLS: &model.HLSSource{URL: "https://example.com/live-a.m3u8"},
		},
	}
	return project
}

func waitForEngineState(t *testing.T, engine *Engine, predicate func(model.StreamStatus) bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := engine.Status()
		if predicate(status) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("engine state did not match before timeout: %+v", engine.Status())
}
