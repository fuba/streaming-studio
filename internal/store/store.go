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
	normalizeOutputSettings(&state.Output, defaults.Output)
	if state.Sources == nil {
		state.Sources = []model.Source{}
	}
	if state.Assets == nil {
		state.Assets = []model.Asset{}
	}
	if state.OutputPresets == nil {
		state.OutputPresets = []model.OutputPreset{}
	}
	for i := range state.OutputPresets {
		normalizeOutputSettings(&state.OutputPresets[i].Settings, defaults.Output)
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

func normalizeOutputSettings(output *model.OutputSettings, defaults model.OutputSettings) {
	if output.Mode == "" {
		output.Mode = defaults.Mode
	}
	if output.FrameRate <= 0 {
		output.FrameRate = defaults.FrameRate
	}
	if output.VideoBitrate == "" {
		output.VideoBitrate = defaults.VideoBitrate
	}
	if output.AudioBitrate == "" {
		output.AudioBitrate = defaults.AudioBitrate
	}
	if output.HLS.SegmentDuration <= 0 {
		output.HLS.SegmentDuration = defaults.HLS.SegmentDuration
	}
	if output.HLS.ListSize <= 0 {
		output.HLS.ListSize = defaults.HLS.ListSize
	}
	if output.HLS.Path == "" {
		output.HLS.Path = defaults.HLS.Path
	}
	if output.HLS.PublicPath == "" {
		output.HLS.PublicPath = defaults.HLS.PublicPath
	}
	if output.YouTube.RTMPURL == "" {
		output.YouTube.RTMPURL = defaults.YouTube.RTMPURL
	}
	if output.YouTube.Preset == "" {
		output.YouTube.Preset = defaults.YouTube.Preset
	}
	if output.AdditionalArgs == nil {
		output.AdditionalArgs = []string{}
	}
	if output.YouTube.AdditionalArgs == nil {
		output.YouTube.AdditionalArgs = []string{}
	}
}
