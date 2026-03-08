package store

import (
	"path/filepath"
	"testing"

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
