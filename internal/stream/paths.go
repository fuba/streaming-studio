package stream

import (
	"fmt"
	"path/filepath"
	"strings"
)

func ResolveOutputPath(dataDir, relativePath string) (string, error) {
	cleanDataDir := filepath.Clean(dataDir)
	cleanRelativePath := filepath.Clean(strings.TrimSpace(relativePath))

	if cleanRelativePath == "" || cleanRelativePath == "." {
		return "", fmt.Errorf("output path is required")
	}
	if filepath.IsAbs(cleanRelativePath) {
		return "", fmt.Errorf("output path must be relative to the data directory")
	}
	if cleanRelativePath == ".." || strings.HasPrefix(cleanRelativePath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output path must stay inside the data directory")
	}

	targetPath := filepath.Join(cleanDataDir, cleanRelativePath)
	relativeToDataDir, err := filepath.Rel(cleanDataDir, targetPath)
	if err != nil {
		return "", err
	}
	if relativeToDataDir == ".." || strings.HasPrefix(relativeToDataDir, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output path must stay inside the data directory")
	}

	return targetPath, nil
}
