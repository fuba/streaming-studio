package stream

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteTextFileSkipsUnchangedContent(t *testing.T) {
	t.Parallel()

	targetPath := filepath.Join(t.TempDir(), "runtime.txt")
	if err := writeTextFile(targetPath, "same content"); err != nil {
		t.Fatalf("writeTextFile() returned error: %v", err)
	}

	before, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("os.Stat(%q) returned error: %v", targetPath, err)
	}

	time.Sleep(20 * time.Millisecond)

	if err := writeTextFile(targetPath, "same content"); err != nil {
		t.Fatalf("writeTextFile() returned error: %v", err)
	}

	after, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("os.Stat(%q) returned error: %v", targetPath, err)
	}

	if !os.SameFile(before, after) {
		t.Fatalf("file identity changed for unchanged content")
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("mod time changed for unchanged content: before=%v after=%v", before.ModTime(), after.ModTime())
	}
}

func TestWriteTextFileReplacesContentAtomically(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	targetPath := filepath.Join(dir, "runtime.txt")

	if err := writeTextFile(targetPath, "before"); err != nil {
		t.Fatalf("writeTextFile() returned error: %v", err)
	}
	if err := writeTextFile(targetPath, "after"); err != nil {
		t.Fatalf("writeTextFile() returned error: %v", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) returned error: %v", targetPath, err)
	}
	if string(content) != "after" {
		t.Fatalf("text file content = %q, want %q", string(content), "after")
	}

	matches, err := filepath.Glob(filepath.Join(dir, ".text-*.tmp"))
	if err != nil {
		t.Fatalf("filepath.Glob() returned error: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}
