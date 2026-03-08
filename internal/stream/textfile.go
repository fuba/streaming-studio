package stream

import (
	"bytes"
	"os"
	"path/filepath"
)

func writeTextFile(path, content string) error {
	payload := []byte(content)

	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, payload) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".text-*.tmp")
	if err != nil {
		return err
	}

	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		_ = tmpFile.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(payload); err != nil {
		return err
	}
	if err := tmpFile.Chmod(0o644); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	cleanup = false
	return nil
}
