package stream

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"streaming-studio/internal/model"
)

type Engine struct {
	dataDir string
	logger  *log.Logger

	mu        sync.RWMutex
	cmd       *exec.Cmd
	status    model.StreamStatus
	ffmpegLog string
}

func NewEngine(dataDir, ffmpegLog string, logger *log.Logger) *Engine {
	return &Engine{
		dataDir:   dataDir,
		ffmpegLog: ffmpegLog,
		logger:    logger,
		status: model.StreamStatus{
			Command: []string{},
		},
	}
}

func (e *Engine) Start(project model.ProjectState) (model.StreamStatus, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cmd != nil && e.cmd.Process != nil {
		return e.status, fmt.Errorf("stream is already running")
	}

	if project.Output.Mode == model.OutputModeHLS {
		hlsDir := filepath.Join(e.dataDir, filepath.Dir(project.Output.HLS.Path))
		if err := os.RemoveAll(hlsDir); err != nil {
			return e.status, err
		}
		if err := os.MkdirAll(hlsDir, 0o755); err != nil {
			return e.status, err
		}
	}

	result, err := BuildFFmpegArgs(project, BuildConfig{DataDir: e.dataDir})
	if err != nil {
		return e.status, err
	}

	if err := os.MkdirAll(filepath.Dir(e.ffmpegLog), 0o755); err != nil {
		return e.status, err
	}

	logFile, err := os.OpenFile(e.ffmpegLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return e.status, err
	}

	cmd := exec.Command("ffmpeg", result.Args...)
	writer := io.MultiWriter(logFile, os.Stdout)
	cmd.Stdout = writer
	cmd.Stderr = writer
	cmd.Dir = e.dataDir

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return e.status, err
	}

	startedAt := time.Now().UTC()
	e.cmd = cmd
	e.status = model.StreamStatus{
		Running:   true,
		Mode:      project.Output.Mode,
		StartedAt: &startedAt,
		PID:       cmd.Process.Pid,
		Command:   append([]string{"ffmpeg"}, result.Args...),
	}

	e.logger.Printf("stream started pid=%d mode=%s", cmd.Process.Pid, project.Output.Mode)

	go e.wait(cmd, logFile)

	return e.status, nil
}

func (e *Engine) Stop() (model.StreamStatus, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cmd == nil || e.cmd.Process == nil {
		return e.status, nil
	}

	if err := e.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return e.status, err
	}

	e.logger.Printf("stream stop requested pid=%d", e.cmd.Process.Pid)
	return e.status, nil
}

func (e *Engine) Status() model.StreamStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return cloneStatus(e.status)
}

func (e *Engine) wait(cmd *exec.Cmd, logFile *os.File) {
	err := cmd.Wait()
	_ = logFile.Close()

	e.mu.Lock()
	defer e.mu.Unlock()

	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}

	e.logger.Printf("stream exited pid=%d err=%v", pid, err)

	e.cmd = nil
	e.status.Running = false
	e.status.PID = 0
	if err != nil {
		e.status.LastError = err.Error()
	} else {
		e.status.LastError = ""
	}
}

func cloneStatus(status model.StreamStatus) model.StreamStatus {
	cloned := status
	cloned.Command = append([]string{}, status.Command...)
	if status.StartedAt != nil {
		startedAt := *status.StartedAt
		cloned.StartedAt = &startedAt
	}
	return cloned
}
