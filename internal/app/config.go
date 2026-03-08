package app

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

type Config struct {
	ListenAddr string
	DataDir    string
	UIDistDir  string
	StatePath  string
	LogPath    string
	FFmpegLog  string
}

func LoadConfig() Config {
	dataDir := getenv("DATA_DIR", "/data")
	return Config{
		ListenAddr: getenv("LISTEN_ADDR", ":8080"),
		DataDir:    dataDir,
		UIDistDir:  getenv("UI_DIST_DIR", "frontend/dist"),
		StatePath:  filepath.Join(dataDir, "state.json"),
		LogPath:    filepath.Join(dataDir, "logs", "server.log"),
		FFmpegLog:  filepath.Join(dataDir, "logs", "ffmpeg.log"),
	}
}

func NewLogger(path string) (*log.Logger, func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, err
	}

	logger := log.New(io.MultiWriter(os.Stdout, file), "", log.LstdFlags|log.Lmicroseconds)
	return logger, func() {
		_ = file.Close()
	}, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
