package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"streaming-studio/internal/model"
	"streaming-studio/internal/store"
	"streaming-studio/internal/stream"
)

type fakeEngine struct {
	running     bool
	startCalls  int
	stopCalls   int
	updateCalls int
	lastProject model.ProjectState
	status      model.StreamStatus
}

func (f *fakeEngine) Start(project model.ProjectState) (model.StreamStatus, error) {
	f.startCalls++
	f.running = true
	f.lastProject = project
	f.status = model.StreamStatus{Running: true, Mode: project.Output.Mode}
	return f.status, nil
}

func (f *fakeEngine) Stop() (model.StreamStatus, error) {
	f.stopCalls++
	f.running = false
	f.status.Running = false
	return f.status, nil
}

func (f *fakeEngine) Status() model.StreamStatus {
	if f.running {
		f.status.Running = true
	}
	return f.status
}

func (f *fakeEngine) UpdateProject(project model.ProjectState) {
	f.updateCalls++
	f.lastProject = project
}

func TestStateEndpointReturnsDefaultProject(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/state", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	var payload model.StateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}
	if payload.Project.Canvas.Width != 1280 {
		t.Fatalf("Canvas.Width = %d, want 1280", payload.Project.Canvas.Width)
	}
}

func TestMiddlewareDoesNotAllowArbitraryCrossOriginAccess(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/state", nil)
	request.Header.Set("Origin", "https://example.invalid")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}

func TestCreateSourceEndpointStoresNewSource(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	source := model.Source{
		ID:      "src-1",
		Name:    "Main Camera",
		Kind:    model.SourceKindHLS,
		Enabled: true,
		Layout: model.Layout{
			X:       10,
			Y:       20,
			Width:   640,
			Height:  360,
			Opacity: 1,
		},
		HLS: &model.HLSSource{URL: "https://example.com/live.m3u8"},
	}

	body, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sources", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}

	stateRequest := httptest.NewRequest(http.MethodGet, "/api/v1/state", nil)
	stateRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(stateRecorder, stateRequest)

	var payload model.StateResponse
	if err := json.Unmarshal(stateRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}
	if len(payload.Project.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want 1", len(payload.Project.Sources))
	}
}

func TestCreateTextSourceRejectsInvalidBackgroundOpacity(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	backgroundOpacity := 1.5
	source := model.Source{
		ID:      "text-1",
		Name:    "Text",
		Kind:    model.SourceKindText,
		Enabled: true,
		Layout: model.Layout{
			Width:   400,
			Height:  120,
			Opacity: 1,
		},
		Text: &model.TextSource{
			Content:           "hello",
			BackgroundOpacity: &backgroundOpacity,
		},
	}

	body, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sources", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func TestStateEndpointUpdatesDesiredProjectWhenStreamStopped(t *testing.T) {
	t.Parallel()

	engine := &fakeEngine{}
	server := newTestServerWithEngine(t, engine)
	project := model.DefaultProjectState()
	project.Sources = []model.Source{
		{
			ID:      "text-1",
			Name:    "Text",
			Kind:    model.SourceKindText,
			Enabled: true,
			Layout: model.Layout{
				Width:   400,
				Height:  120,
				Opacity: 1,
			},
			Text: &model.TextSource{Content: "updated"},
		},
	}

	body, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/state", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if engine.updateCalls != 1 {
		t.Fatalf("updateCalls = %d, want 1", engine.updateCalls)
	}
	if engine.startCalls != 0 {
		t.Fatalf("startCalls = %d, want 0", engine.startCalls)
	}
	if len(engine.lastProject.Sources) != 1 || engine.lastProject.Sources[0].ID != "text-1" {
		t.Fatalf("lastProject = %+v, want updated project", engine.lastProject)
	}
}

func TestStateEndpointRejectsDuplicateSourceIDs(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	project := model.DefaultProjectState()
	project.Sources = []model.Source{
		{
			ID:      "dup",
			Name:    "Camera A",
			Kind:    model.SourceKindHLS,
			Enabled: true,
			Layout:  model.Layout{Width: 640, Height: 360, Opacity: 1},
			HLS:     &model.HLSSource{URL: "https://example.com/live-a.m3u8"},
		},
		{
			ID:      "dup",
			Name:    "Camera B",
			Kind:    model.SourceKindHLS,
			Enabled: true,
			Layout:  model.Layout{Width: 640, Height: 360, Opacity: 1},
			HLS:     &model.HLSSource{URL: "https://example.com/live-b.m3u8"},
		},
	}

	body, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/state", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func TestStateEndpointRejectsUnknownAudioSourceID(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	project := model.DefaultProjectState()
	project.Output.AudioSourceID = "missing"
	project.Sources = []model.Source{
		{
			ID:      "cam-a",
			Name:    "Camera A",
			Kind:    model.SourceKindHLS,
			Enabled: true,
			Layout:  model.Layout{Width: 640, Height: 360, Opacity: 1},
			HLS:     &model.HLSSource{URL: "https://example.com/live-a.m3u8"},
		},
	}

	body, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/state", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func TestStateEndpointRejectsUnsafeHLSOutputPath(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	project := model.DefaultProjectState()
	project.Output.HLS.Path = "../../../tmp/live.m3u8"

	body, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/state", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func TestStateEndpointRejectsAbsoluteHLSPublicPath(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	project := model.DefaultProjectState()
	project.Output.HLS.PublicPath = "http://example.com/live/live.m3u8"

	body, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/state", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func TestStateEndpointRejectsDuplicateOutputPresetIDs(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	project := model.DefaultProjectState()
	project.OutputPresets = []model.OutputPreset{
		{ID: "preset-1", Name: "Primary", Settings: project.Output},
		{ID: "preset-1", Name: "Backup", Settings: project.Output},
	}

	body, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/state", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func TestStateEndpointRejectsUnsafeOutputPresetPath(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	project := model.DefaultProjectState()
	presetSettings := project.Output
	presetSettings.HLS.Path = "../../../tmp/live.m3u8"
	project.OutputPresets = []model.OutputPreset{
		{ID: "preset-1", Name: "Unsafe", Settings: presetSettings},
	}

	body, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/state", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateSourceEndpointAllowsDisabledDraftHLS(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	source := model.Source{
		Name:    "Draft Camera",
		Kind:    model.SourceKindHLS,
		Enabled: false,
		Layout: model.Layout{
			Width:   640,
			Height:  360,
			Opacity: 1,
		},
		HLS: &model.HLSSource{},
	}

	body, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sources", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateSourceEndpointKeepsExplicitZeroOpacity(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	source := model.Source{
		ID:      "src-transparent",
		Name:    "Transparent Overlay",
		Kind:    model.SourceKindHLS,
		Enabled: true,
		Layout: model.Layout{
			Width:   640,
			Height:  360,
			Opacity: 0,
		},
		HLS: &model.HLSSource{URL: "https://example.com/live.m3u8"},
	}

	body, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sources", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}

	var payload model.StateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}
	if payload.Project.Sources[0].Layout.Opacity != 0 {
		t.Fatalf("Opacity = %v, want 0", payload.Project.Sources[0].Layout.Opacity)
	}
}

func TestCreateSourceEndpointAllowsRemoteTextWithoutStaticContent(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	source := model.Source{
		ID:      "remote-text-1",
		Name:    "Remote Text",
		Kind:    model.SourceKindText,
		Enabled: true,
		Layout: model.Layout{
			Width:   640,
			Height:  120,
			Opacity: 1,
		},
		Text: &model.TextSource{
			Remote: &model.RemoteTextSource{
				URL:                    "http://example.com/info.txt",
				RefreshIntervalSeconds: 3,
			},
		},
	}

	body, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sources", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
}

func TestStateEndpointRestartsRunningStreamOnSave(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	logger := log.New(bytes.NewBuffer(nil), "", 0)
	engine := &fakeEngine{running: true, status: model.StreamStatus{Running: true, Mode: model.OutputModeHLS}}
	server := NewServer(stateStore, engine, dataDir, filepath.Join(dataDir, "dist"), logger)

	project := model.DefaultProjectState()
	project.Canvas.Width = 1920

	body, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/state", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if engine.stopCalls != 1 || engine.startCalls != 1 {
		t.Fatalf("restart calls = stop:%d start:%d, want stop:1 start:1", engine.stopCalls, engine.startCalls)
	}
}

func TestSourceUpdateRestartsRunningStreamOnSave(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	logger := log.New(bytes.NewBuffer(nil), "", 0)
	engine := &fakeEngine{running: true, status: model.StreamStatus{Running: true, Mode: model.OutputModeHLS}}
	server := NewServer(stateStore, engine, dataDir, filepath.Join(dataDir, "dist"), logger)

	project := model.DefaultProjectState()
	project.Sources = []model.Source{
		{
			ID:      "source-1",
			Name:    "Camera",
			Kind:    model.SourceKindHLS,
			Enabled: true,
			Layout:  model.Layout{Width: 640, Height: 360, Opacity: 1},
			HLS:     &model.HLSSource{URL: "https://example.com/live.m3u8"},
		},
	}
	if err := stateStore.Save(project); err != nil {
		t.Fatalf("stateStore.Save() returned error: %v", err)
	}

	project.Sources[0].Layout.X = 120
	body, err := json.Marshal(project.Sources[0])
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/sources/source-1", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if engine.stopCalls != 1 || engine.startCalls != 1 {
		t.Fatalf("restart calls = stop:%d start:%d, want stop:1 start:1", engine.stopCalls, engine.startCalls)
	}
}

func TestDeleteSourceClearsOutputPresetAudioSourceID(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	project := model.DefaultProjectState()
	project.Sources = []model.Source{
		{
			ID:      "source-1",
			Name:    "Camera",
			Kind:    model.SourceKindHLS,
			Enabled: true,
			Layout:  model.Layout{Width: 640, Height: 360, Opacity: 1},
			HLS:     &model.HLSSource{URL: "https://example.com/live.m3u8"},
		},
	}
	project.Output.AudioSourceID = "source-1"
	presetSettings := project.Output
	presetSettings.AudioSourceID = "source-1"
	project.OutputPresets = []model.OutputPreset{
		{ID: "preset-1", Name: "Preset", Settings: presetSettings},
	}

	body, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}
	saveRequest := httptest.NewRequest(http.MethodPut, "/api/v1/state", bytes.NewReader(body))
	saveRequest.Header.Set("Content-Type", "application/json")
	saveRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(saveRecorder, saveRequest)
	if saveRecorder.Code != http.StatusOK {
		t.Fatalf("save status = %d, want 200: %s", saveRecorder.Code, saveRecorder.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/sources/source-1", nil)
	deleteRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteRecorder, deleteRequest)

	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	var payload model.StateResponse
	if err := json.Unmarshal(deleteRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}
	if payload.Project.Output.AudioSourceID != "" {
		t.Fatalf("Output.AudioSourceID = %q, want empty", payload.Project.Output.AudioSourceID)
	}
	if payload.Project.OutputPresets[0].Settings.AudioSourceID != "" {
		t.Fatalf("preset AudioSourceID = %q, want empty", payload.Project.OutputPresets[0].Settings.AudioSourceID)
	}
}

func TestRuntimeTextsEndpointReturnsResolvedText(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	logger := log.New(bytes.NewBuffer(nil), "", 0)
	engine := &fakeEngine{}
	server := NewServer(stateStore, engine, dataDir, filepath.Join(dataDir, "dist"), logger)

	project := model.DefaultProjectState()
	project.Sources = []model.Source{
		{
			ID:      "text-1",
			Name:    "Remote Text",
			Kind:    model.SourceKindText,
			Enabled: true,
			Layout:  model.Layout{Width: 320, Height: 120, Opacity: 1},
			Text: &model.TextSource{
				Content: "fallback",
			},
		},
	}
	if err := stateStore.Save(project); err != nil {
		t.Fatalf("stateStore.Save() returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "runtime", "text"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "runtime", "text", "text-1.txt"), []byte("resolved"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/texts", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}
	if payload["text-1"] != "resolved" {
		t.Fatalf("payload[text-1] = %q, want resolved", payload["text-1"])
	}
}

func TestLogsEndpointReturnsTailLines(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	logger := log.New(bytes.NewBuffer(nil), "", 0)
	engine := &fakeEngine{}
	server := NewServer(stateStore, engine, dataDir, filepath.Join(dataDir, "dist"), logger)

	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() returned error: %v", err)
	}
	serverLog := filepath.Join(dataDir, "logs", "server.log")
	if err := os.WriteFile(serverLog, []byte("line-1\nline-2\nline-3\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/logs?target=server&lines=2", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Target    string   `json:"target"`
		Lines     []string `json:"lines"`
		Truncated bool     `json:"truncated"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}
	if payload.Target != "server" {
		t.Fatalf("target = %q, want server", payload.Target)
	}
	if len(payload.Lines) != 2 || payload.Lines[0] != "line-2" || payload.Lines[1] != "line-3" {
		t.Fatalf("lines = %#v, want [line-2 line-3]", payload.Lines)
	}
	if !payload.Truncated {
		t.Fatalf("truncated = false, want true")
	}
}

func TestLogsEndpointRedactsYouTubeStreamKey(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	project := model.DefaultProjectState()
	project.Output.Mode = model.OutputModeYouTube
	project.Output.YouTube.StreamKey = "secret-stream-key"
	if err := stateStore.Save(project); err != nil {
		t.Fatalf("stateStore.Save() returned error: %v", err)
	}
	logger := log.New(bytes.NewBuffer(nil), "", 0)
	engine := &fakeEngine{}
	server := NewServer(stateStore, engine, dataDir, filepath.Join(dataDir, "dist"), logger)

	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() returned error: %v", err)
	}
	ffmpegLog := filepath.Join(dataDir, "logs", "ffmpeg.log")
	logBody := "Error writing trailer of rtmp://a.rtmp.youtube.com/live2/secret-stream-key: Broken pipe\n"
	if err := os.WriteFile(ffmpegLog, []byte(logBody), 0o644); err != nil {
		t.Fatalf("os.WriteFile() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/logs?target=ffmpeg&lines=10", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}
	body := strings.Join(payload.Lines, "\n")
	if strings.Contains(body, "secret-stream-key") {
		t.Fatalf("log response leaked stream key: %s", body)
	}
	if !strings.Contains(body, "[REDACTED_STREAM_KEY]") {
		t.Fatalf("log response = %q, want redaction marker", body)
	}
}

func TestLogsEndpointRejectsUnknownTarget(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/logs?target=invalid", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeStreamHealthEndpointReturnsWarning(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	logger := log.New(bytes.NewBuffer(nil), "", 0)
	engine := &fakeEngine{running: true, status: model.StreamStatus{Running: true, Mode: model.OutputModeYouTube}}
	server := NewServer(stateStore, engine, dataDir, filepath.Join(dataDir, "dist"), logger)

	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() returned error: %v", err)
	}
	ffmpegLogPath := filepath.Join(dataDir, "logs", "ffmpeg.log")
	logBody := "frame=1 fps=30\n[tcp @ 0x1] Connection to tcp://kirgizu:5000 failed: Connection refused\n"
	if err := os.WriteFile(ffmpegLogPath, []byte(logBody), 0o644); err != nil {
		t.Fatalf("os.WriteFile() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/stream-health", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}
	if payload["status"] != "warning" {
		t.Fatalf("status = %#v, want warning", payload["status"])
	}
}

func TestRuntimeStreamHealthEndpointReturnsError(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	logger := log.New(bytes.NewBuffer(nil), "", 0)
	engine := &fakeEngine{running: true, status: model.StreamStatus{Running: true, Mode: model.OutputModeYouTube}}
	server := NewServer(stateStore, engine, dataDir, filepath.Join(dataDir, "dist"), logger)

	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() returned error: %v", err)
	}
	ffmpegLogPath := filepath.Join(dataDir, "logs", "ffmpeg.log")
	logBody := "av_interleaved_write_frame(): Broken pipe\nError writing trailer of rtmp://a.rtmp.youtube.com/live2/xxx: Broken pipe\n"
	if err := os.WriteFile(ffmpegLogPath, []byte(logBody), 0o644); err != nil {
		t.Fatalf("os.WriteFile() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/stream-health", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}
	if payload["status"] != "error" {
		t.Fatalf("status = %#v, want error", payload["status"])
	}
}

func TestRuntimeStreamHealthEndpointRedactsYouTubeStreamKey(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	project := model.DefaultProjectState()
	project.Output.Mode = model.OutputModeYouTube
	project.Output.YouTube.StreamKey = "secret-health-key"
	if err := stateStore.Save(project); err != nil {
		t.Fatalf("stateStore.Save() returned error: %v", err)
	}
	logger := log.New(bytes.NewBuffer(nil), "", 0)
	engine := &fakeEngine{running: true, status: model.StreamStatus{Running: true, Mode: model.OutputModeYouTube}}
	server := NewServer(stateStore, engine, dataDir, filepath.Join(dataDir, "dist"), logger)

	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() returned error: %v", err)
	}
	ffmpegLogPath := filepath.Join(dataDir, "logs", "ffmpeg.log")
	logBody := "Error writing trailer of rtmp://a.rtmp.youtube.com/live2/secret-health-key: Broken pipe\n"
	if err := os.WriteFile(ffmpegLogPath, []byte(logBody), 0o644); err != nil {
		t.Fatalf("os.WriteFile() returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/stream-health", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		CriticalEvents []string `json:"criticalEvents"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}
	body := strings.Join(payload.CriticalEvents, "\n")
	if strings.Contains(body, "secret-health-key") {
		t.Fatalf("health response leaked stream key: %s", body)
	}
	if !strings.Contains(body, "[REDACTED_STREAM_KEY]") {
		t.Fatalf("health response = %q, want redaction marker", body)
	}
}

func TestStreamStartStopPersistRunState(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	logger := log.New(bytes.NewBuffer(nil), "", 0)
	engine := &fakeEngine{}
	server := NewServer(stateStore, engine, dataDir, filepath.Join(dataDir, "dist"), logger)

	startRequest := httptest.NewRequest(http.MethodPost, "/api/v1/stream/start", nil)
	startRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(startRecorder, startRequest)
	if startRecorder.Code != http.StatusOK {
		t.Fatalf("start status = %d, want 200: %s", startRecorder.Code, startRecorder.Body.String())
	}

	runStatePath := filepath.Join(dataDir, "runtime", "stream-run-state.json")
	runStateRaw, err := os.ReadFile(runStatePath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) returned error: %v", runStatePath, err)
	}
	var runState map[string]any
	if err := json.Unmarshal(runStateRaw, &runState); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}
	if shouldRun, ok := runState["shouldRun"].(bool); !ok || !shouldRun {
		t.Fatalf("shouldRun = %#v, want true", runState["shouldRun"])
	}

	stopRequest := httptest.NewRequest(http.MethodPost, "/api/v1/stream/stop", nil)
	stopRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(stopRecorder, stopRequest)
	if stopRecorder.Code != http.StatusOK {
		t.Fatalf("stop status = %d, want 200: %s", stopRecorder.Code, stopRecorder.Body.String())
	}

	runStateRaw, err = os.ReadFile(runStatePath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) returned error: %v", runStatePath, err)
	}
	if err := json.Unmarshal(runStateRaw, &runState); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}
	if shouldRun, ok := runState["shouldRun"].(bool); !ok || shouldRun {
		t.Fatalf("shouldRun = %#v, want false", runState["shouldRun"])
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	logger := log.New(bytes.NewBuffer(nil), "", 0)
	engine := stream.NewEngine(dataDir, filepath.Join(dataDir, "ffmpeg.log"), logger)

	return NewServer(stateStore, engine, dataDir, filepath.Join(dataDir, "dist"), logger)
}

func newTestServerWithEngine(t *testing.T, engine StreamController) *Server {
	t.Helper()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	logger := log.New(bytes.NewBuffer(nil), "", 0)

	return NewServer(stateStore, engine, dataDir, filepath.Join(dataDir, "dist"), logger)
}
