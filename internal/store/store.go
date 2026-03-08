package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"streaming-studio/internal/model"
)

type FileStore struct {
	path string
	mu   sync.RWMutex
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Load() (model.ProjectState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return model.ProjectState{}, err
	}

	buf, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		state := model.DefaultProjectState()
		if err := s.saveLocked(state); err != nil {
			return model.ProjectState{}, err
		}
		return state, nil
	}
	if err != nil {
		return model.ProjectState{}, err
	}

	var state model.ProjectState
	if err := json.Unmarshal(buf, &state); err != nil {
		return model.ProjectState{}, err
	}

	normalizeState(&state)
	return state, nil
}

func (s *FileStore) Save(state model.ProjectState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalizeState(&state)
	return s.saveLocked(state)
}

func (s *FileStore) Update(fn func(*model.ProjectState) error) (model.ProjectState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.loadLocked()
	if err != nil {
		return model.ProjectState{}, err
	}
	if err := fn(&state); err != nil {
		return model.ProjectState{}, err
	}
	normalizeState(&state)
	if err := s.saveLocked(state); err != nil {
		return model.ProjectState{}, err
	}
	return state, nil
}

func (s *FileStore) saveLocked(state model.ProjectState) error {
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return err
	}

	return os.Rename(tmpPath, s.path)
}

func (s *FileStore) loadLocked() (model.ProjectState, error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return model.ProjectState{}, err
	}

	buf, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		state := model.DefaultProjectState()
		if err := s.saveLocked(state); err != nil {
			return model.ProjectState{}, err
		}
		return state, nil
	}
	if err != nil {
		return model.ProjectState{}, err
	}

	var state model.ProjectState
	if err := json.Unmarshal(buf, &state); err != nil {
		return model.ProjectState{}, err
	}

	normalizeState(&state)
	return state, nil
}

func normalizeState(state *model.ProjectState) {
	defaults := model.DefaultProjectState()

	if state.Canvas.Width <= 0 {
		state.Canvas.Width = defaults.Canvas.Width
	}
	if state.Canvas.Height <= 0 {
		state.Canvas.Height = defaults.Canvas.Height
	}
	if state.Canvas.BackgroundColor == "" {
		state.Canvas.BackgroundColor = defaults.Canvas.BackgroundColor
	}
	if state.Canvas.EditorBackgroundColor == "" {
		state.Canvas.EditorBackgroundColor = defaults.Canvas.EditorBackgroundColor
	}
	if state.Output.Mode == "" {
		state.Output.Mode = defaults.Output.Mode
	}
	if state.Output.FrameRate <= 0 {
		state.Output.FrameRate = defaults.Output.FrameRate
	}
	if state.Output.VideoBitrate == "" {
		state.Output.VideoBitrate = defaults.Output.VideoBitrate
	}
	if state.Output.AudioBitrate == "" {
		state.Output.AudioBitrate = defaults.Output.AudioBitrate
	}
	if state.Output.HLS.SegmentDuration <= 0 {
		state.Output.HLS.SegmentDuration = defaults.Output.HLS.SegmentDuration
	}
	if state.Output.HLS.ListSize <= 0 {
		state.Output.HLS.ListSize = defaults.Output.HLS.ListSize
	}
	if state.Output.HLS.Path == "" {
		state.Output.HLS.Path = defaults.Output.HLS.Path
	}
	if state.Output.HLS.PublicPath == "" {
		state.Output.HLS.PublicPath = defaults.Output.HLS.PublicPath
	}
	if state.Output.YouTube.RTMPURL == "" {
		state.Output.YouTube.RTMPURL = defaults.Output.YouTube.RTMPURL
	}
	if state.Output.YouTube.Preset == "" {
		state.Output.YouTube.Preset = defaults.Output.YouTube.Preset
	}
	if state.Sources == nil {
		state.Sources = []model.Source{}
	}
	if state.Assets == nil {
		state.Assets = []model.Asset{}
	}
	if state.Output.AdditionalArgs == nil {
		state.Output.AdditionalArgs = []string{}
	}
	if state.Output.YouTube.AdditionalArgs == nil {
		state.Output.YouTube.AdditionalArgs = []string{}
	}
	for i := range state.Sources {
		if state.Sources[i].Text != nil && state.Sources[i].Text.BackgroundOpacity == nil {
			defaultOpacity := 0.8
			state.Sources[i].Text.BackgroundOpacity = &defaultOpacity
		}
		if state.Sources[i].Text != nil && state.Sources[i].Text.Remote != nil && state.Sources[i].Text.Remote.RefreshIntervalSeconds < 0 {
			state.Sources[i].Text.Remote.RefreshIntervalSeconds = 0
		}
		if state.Sources[i].Layout.Radius < 0 {
			state.Sources[i].Layout.Radius = 0
		}
		if state.Sources[i].Layout.Opacity < 0 {
			state.Sources[i].Layout.Opacity = 0
		}
		if state.Sources[i].Layout.Opacity > 1 {
			state.Sources[i].Layout.Opacity = 1
		}
		if state.Sources[i].Layout.Width <= 0 {
			state.Sources[i].Layout.Width = 320
		}
		if state.Sources[i].Layout.Height <= 0 {
			state.Sources[i].Layout.Height = 180
		}
	}
}
