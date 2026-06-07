package store

import (
	"path/filepath"
	"testing"
	"time"

	"streaming-studio/internal/model"
)

func TestFileStorePersistsAndReloadsState(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	store := NewFileStore(path)

	initial, err := store.Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if initial.Output.YouTube.Preset != "youtube-default" {
		t.Fatalf("default preset = %q, want youtube-default", initial.Output.YouTube.Preset)
	}

	initial.Canvas.Width = 1920
	initial.Sources = append(initial.Sources, model.Source{
		ID:      "source-1",
		Name:    "Main",
		Kind:    model.SourceKindHLS,
		Enabled: true,
		Layout: model.Layout{
			X:       12,
			Y:       34,
			Width:   640,
			Height:  360,
			Radius:  24,
			Opacity: 1,
			ZIndex:  1,
		},
		HLS: &model.HLSSource{URL: "https://example.com/live.m3u8"},
	})

	if err := store.Save(initial); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after save returned error: %v", err)
	}

	if reloaded.Canvas.Width != 1920 {
		t.Fatalf("Canvas.Width = %d, want 1920", reloaded.Canvas.Width)
	}
	if len(reloaded.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want 1", len(reloaded.Sources))
	}
	if reloaded.Sources[0].HLS == nil || reloaded.Sources[0].HLS.URL == "" {
		t.Fatalf("reloaded HLS source was lost: %#v", reloaded.Sources[0])
	}
	if reloaded.Sources[0].Layout.Radius != 24 {
		t.Fatalf("Layout.Radius = %d, want 24", reloaded.Sources[0].Layout.Radius)
	}
}

func TestFileStorePersistsOutputPresets(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	store := NewFileStore(path)
	state := model.DefaultProjectState()
	updatedAt := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	state.ActiveOutputPresetID = "preset-live"
	state.OutputPresets = []model.OutputPreset{
		{
			ID:        "preset-live",
			Name:      "Live",
			Settings:  state.Output,
			CreatedAt: updatedAt,
			UpdatedAt: updatedAt,
		},
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after save returned error: %v", err)
	}

	if reloaded.ActiveOutputPresetID != "preset-live" {
		t.Fatalf("ActiveOutputPresetID = %q, want preset-live", reloaded.ActiveOutputPresetID)
	}
	if len(reloaded.OutputPresets) != 1 {
		t.Fatalf("len(OutputPresets) = %d, want 1", len(reloaded.OutputPresets))
	}
	if reloaded.OutputPresets[0].Name != "Live" {
		t.Fatalf("OutputPresets[0].Name = %q, want Live", reloaded.OutputPresets[0].Name)
	}
	if reloaded.OutputPresets[0].Settings.YouTube.Preset != "youtube-default" {
		t.Fatalf("preset YouTube preset = %q, want youtube-default", reloaded.OutputPresets[0].Settings.YouTube.Preset)
	}
}

func TestFileStoreMirrorsStateToBackupPath(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "data", "state.json")
	backupPath := filepath.Join(tempDir, "backup", "streaming-studio", "project-state", "state.json")
	store := NewFileStoreWithBackup(path, backupPath)
	state := model.DefaultProjectState()
	state.Canvas.Width = 1920

	if err := store.Save(state); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	backupStore := NewFileStore(backupPath)
	reloaded, err := backupStore.Load()
	if err != nil {
		t.Fatalf("backup Load() returned error: %v", err)
	}
	if reloaded.Canvas.Width != 1920 {
		t.Fatalf("backup Canvas.Width = %d, want 1920", reloaded.Canvas.Width)
	}
}
