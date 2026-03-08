package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"streaming-studio/internal/model"
	"streaming-studio/internal/store"
	"streaming-studio/internal/stream"
)

type Server struct {
	store     *store.FileStore
	engine    StreamController
	dataDir   string
	uiDistDir string
	logger    *log.Logger
}

type StreamController interface {
	Start(project model.ProjectState) (model.StreamStatus, error)
	Stop() (model.StreamStatus, error)
	Status() model.StreamStatus
	UpdateProject(project model.ProjectState)
}

func NewServer(store *store.FileStore, engine StreamController, dataDir, uiDistDir string, logger *log.Logger) *Server {
	return &Server{
		store:     store,
		engine:    engine,
		dataDir:   dataDir,
		uiDistDir: uiDistDir,
		logger:    logger,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/v1/state", s.handleState)
	mux.HandleFunc("/api/v1/sources", s.handleSources)
	mux.HandleFunc("/api/v1/sources/", s.handleSourceByID)
	mux.HandleFunc("/api/v1/runtime/texts", s.handleRuntimeTexts)
	mux.HandleFunc("/api/v1/assets/images", s.handleImageUpload)
	mux.HandleFunc("/api/v1/assets/fonts", s.handleFontUpload)
	mux.HandleFunc("/api/v1/stream/start", s.handleStreamStart)
	mux.HandleFunc("/api/v1/stream/stop", s.handleStreamStop)
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(filepath.Join(s.dataDir, "assets")))))
	mux.Handle("/live/", http.StripPrefix("/live/", http.FileServer(http.Dir(filepath.Join(s.dataDir, "output/hls")))))
	mux.Handle("/", s.uiHandler())

	return s.withMiddleware(mux)
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		started := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(started).String())
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		project, err := s.store.Load()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, model.StateResponse{Project: project, Stream: s.engine.Status()})
	case http.MethodPut:
		var payload model.ProjectState
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validateProjectState(s.dataDir, payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.store.Save(payload); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		project, err := s.store.Load()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		status, err := s.syncStream(project)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, model.StateResponse{Project: project, Stream: status})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	source, opacityProvided, err := decodeSourceCreateRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if source.ID == "" {
		source.ID = newID("src")
	}
	if source.Layout.Width == 0 {
		source.Layout.Width = 320
	}
	if source.Layout.Height == 0 {
		source.Layout.Height = 180
	}
	if !opacityProvided {
		source.Layout.Opacity = 1
	}
	if err := validateSource(source); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	project, err := s.store.Update(func(state *model.ProjectState) error {
		for _, existing := range state.Sources {
			if existing.ID == source.ID {
				return fmt.Errorf("source %s already exists", source.ID)
			}
		}
		state.Sources = append(state.Sources, source)
		slices.SortFunc(state.Sources, func(a, b model.Source) int {
			return a.Layout.ZIndex - b.Layout.ZIndex
		})
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	status, err := s.syncStream(project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusCreated, model.StateResponse{Project: project, Stream: status})
}

func (s *Server) handleSourceByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/sources/")
	if id == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("source id is required"))
		return
	}

	switch r.Method {
	case http.MethodPut:
		var source model.Source
		if err := json.NewDecoder(r.Body).Decode(&source); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		source.ID = id
		if err := validateSource(source); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		project, err := s.store.Update(func(state *model.ProjectState) error {
			for i := range state.Sources {
				if state.Sources[i].ID == id {
					state.Sources[i] = source
					return nil
				}
			}
			return fmt.Errorf("source %s was not found", id)
		})
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		status, err := s.syncStream(project)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, model.StateResponse{Project: project, Stream: status})
	case http.MethodDelete:
		project, err := s.store.Update(func(state *model.ProjectState) error {
			index := -1
			for i := range state.Sources {
				if state.Sources[i].ID == id {
					index = i
					break
				}
			}
			if index == -1 {
				return fmt.Errorf("source %s was not found", id)
			}
			state.Sources = append(state.Sources[:index], state.Sources[index+1:]...)
			if state.Output.AudioSourceID == id {
				state.Output.AudioSourceID = ""
			}
			return nil
		})
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		status, err := s.syncStream(project)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, model.StateResponse{Project: project, Stream: status})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleImageUpload(w http.ResponseWriter, r *http.Request) {
	s.handleAssetUpload(w, r, model.AssetKindImage, "images")
}

func (s *Server) handleFontUpload(w http.ResponseWriter, r *http.Request) {
	s.handleAssetUpload(w, r, model.AssetKindFont, "fonts")
}

func (s *Server) handleAssetUpload(w http.ResponseWriter, r *http.Request, kind model.AssetKind, folder string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer file.Close()

	asset, err := s.persistUpload(file, header, kind, folder)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	project, err := s.store.Update(func(state *model.ProjectState) error {
		state.Assets = append(state.Assets, asset)
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"asset":   asset,
		"project": project,
		"stream":  s.engine.Status(),
	})
}

func (s *Server) handleRuntimeTexts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	project, err := s.store.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	payload := make(map[string]string, len(project.Sources))
	for _, source := range project.Sources {
		if source.Kind != model.SourceKindText || source.Text == nil {
			continue
		}
		runtimePath := filepath.Join(s.dataDir, "runtime", "text", source.ID+".txt")
		content, err := os.ReadFile(runtimePath)
		if err == nil {
			payload[source.ID] = string(content)
			continue
		}
		payload[source.ID] = source.Text.Content
	}

	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) persistUpload(file multipart.File, header *multipart.FileHeader, kind model.AssetKind, folder string) (model.Asset, error) {
	id := newID(string(kind))
	name := sanitizeName(header.Filename)
	extension := filepath.Ext(name)
	fileName := id + extension
	relativePath := filepath.Join("assets", folder, fileName)
	absolutePath := filepath.Join(s.dataDir, relativePath)

	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		return model.Asset{}, err
	}

	destination, err := os.Create(absolutePath)
	if err != nil {
		return model.Asset{}, err
	}
	defer destination.Close()

	if _, err := io.Copy(destination, file); err != nil {
		return model.Asset{}, err
	}

	return model.Asset{
		ID:        id,
		Kind:      kind,
		Name:      header.Filename,
		FileName:  fileName,
		Path:      relativePath,
		URL:       "/uploads/" + folder + "/" + fileName,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (s *Server) handleStreamStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	project, err := s.store.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	status, err := s.engine.Start(project)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, model.StateResponse{Project: project, Stream: status})
}

func (s *Server) handleStreamStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	project, err := s.store.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	status, err := s.engine.Stop()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, model.StateResponse{Project: project, Stream: status})
}

func (s *Server) uiHandler() http.Handler {
	indexPath := filepath.Join(s.uiDistDir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestPath := strings.TrimPrefix(filepath.Clean(r.URL.Path), string(filepath.Separator))
			requestedPath := filepath.Join(s.uiDistDir, requestPath)
			if info, err := os.Stat(requestedPath); err == nil && !info.IsDir() {
				http.ServeFile(w, r, requestedPath)
				return
			}
			http.ServeFile(w, r, indexPath)
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><meta charset="utf-8"><title>Streaming Studio</title></head><body><h1>Streaming Studio</h1><p>Frontend is not built yet. Run docker compose up --build or build the Svelte app into frontend/dist.</p></body></html>`)
	})
}

func validateProjectState(dataDir string, project model.ProjectState) error {
	if project.Canvas.Width <= 0 || project.Canvas.Height <= 0 {
		return fmt.Errorf("canvas width and height must be positive")
	}
	if project.Output.Mode == model.OutputModeHLS {
		if _, err := stream.ResolveOutputPath(dataDir, project.Output.HLS.Path); err != nil {
			return fmt.Errorf("invalid hls output path: %w", err)
		}
	}
	seenSourceIDs := make(map[string]struct{}, len(project.Sources))
	hlsSourceIDs := make(map[string]struct{}, len(project.Sources))
	for _, source := range project.Sources {
		if _, exists := seenSourceIDs[source.ID]; exists {
			return fmt.Errorf("duplicate source id %s", source.ID)
		}
		seenSourceIDs[source.ID] = struct{}{}
		if err := validateSource(source); err != nil {
			return err
		}
		if source.Kind == model.SourceKindHLS {
			hlsSourceIDs[source.ID] = struct{}{}
		}
	}
	if project.Output.AudioSourceID != "" {
		if _, ok := hlsSourceIDs[project.Output.AudioSourceID]; !ok {
			return fmt.Errorf("audioSourceId %s does not reference an HLS source", project.Output.AudioSourceID)
		}
	}
	return nil
}

func validateSource(source model.Source) error {
	if source.ID == "" {
		return fmt.Errorf("source id is required")
	}
	if source.Name == "" {
		return fmt.Errorf("source name is required")
	}
	switch source.Kind {
	case model.SourceKindHLS:
		if source.HLS == nil {
			return fmt.Errorf("hls source config is required")
		}
		if source.Enabled && source.HLS.URL == "" {
			return fmt.Errorf("enabled hls source url is required")
		}
	case model.SourceKindImage:
		if source.Image == nil || source.Image.AssetID == "" {
			return fmt.Errorf("image source assetId is required")
		}
	case model.SourceKindText:
		if source.Text == nil {
			return fmt.Errorf("text source config is required")
		}
		if source.Text.BackgroundOpacity != nil && (*source.Text.BackgroundOpacity < 0 || *source.Text.BackgroundOpacity > 1) {
			return fmt.Errorf("text background opacity must be between 0 and 1")
		}
		hasContent := strings.TrimSpace(source.Text.Content) != ""
		hasRemote := source.Text.Remote != nil && strings.TrimSpace(source.Text.Remote.URL) != ""
		if !hasContent && !hasRemote {
			return fmt.Errorf("text source content or remote url is required")
		}
		if hasRemote {
			if _, err := neturl.ParseRequestURI(source.Text.Remote.URL); err != nil {
				return fmt.Errorf("text remote url is invalid: %w", err)
			}
			if source.Text.Remote.RefreshIntervalSeconds < 0 {
				return fmt.Errorf("text remote refresh interval must be zero or positive")
			}
		}
	default:
		return fmt.Errorf("unsupported source kind %q", source.Kind)
	}
	if source.Layout.Width <= 0 || source.Layout.Height <= 0 {
		return fmt.Errorf("source width and height must be positive")
	}
	if source.Layout.Radius < 0 {
		return fmt.Errorf("source radius must be zero or positive")
	}
	if source.Layout.Opacity < 0 || source.Layout.Opacity > 1 {
		return fmt.Errorf("source opacity must be between 0 and 1")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
}

func newID(prefix string) string {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(raw)
}

func sanitizeName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	if name == "." || name == "" {
		return "upload.bin"
	}
	return name
}

func decodeSourceCreateRequest(r *http.Request) (model.Source, bool, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return model.Source{}, false, err
	}

	var source model.Source
	if err := json.Unmarshal(body, &source); err != nil {
		return model.Source{}, false, err
	}

	var shape struct {
		Layout struct {
			Opacity *float64 `json:"opacity"`
		} `json:"layout"`
	}
	if err := json.Unmarshal(body, &shape); err != nil {
		return model.Source{}, false, err
	}

	return source, shape.Layout.Opacity != nil, nil
}

func (s *Server) syncStream(project model.ProjectState) (model.StreamStatus, error) {
	status := s.engine.Status()
	if !status.Running {
		s.engine.UpdateProject(project)
		return status, nil
	}

	if _, err := s.engine.Stop(); err != nil {
		return s.engine.Status(), err
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status = s.engine.Status()
		if !status.Running {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	status = s.engine.Status()
	if status.Running {
		return status, fmt.Errorf("stream did not stop before reload")
	}

	return s.engine.Start(project)
}
