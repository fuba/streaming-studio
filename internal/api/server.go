package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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

type streamRunState struct {
	ShouldRun bool      `json:"shouldRun"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type runtimeStreamHealth struct {
	Running            bool             `json:"running"`
	StreamMode         model.OutputMode `json:"streamMode"`
	Status             string           `json:"status"`
	Message            string           `json:"message"`
	LogUpdatedAt       *time.Time       `json:"logUpdatedAt,omitempty"`
	AnalysisLineLimit  int              `json:"analysisLineLimit"`
	CriticalEventCount int              `json:"criticalEventCount"`
	WarningEventCount  int              `json:"warningEventCount"`
	CriticalEvents     []string         `json:"criticalEvents"`
	WarningEvents      []string         `json:"warningEvents"`
}

type StreamController interface {
	Start(project model.ProjectState) (model.StreamStatus, error)
	Stop() (model.StreamStatus, error)
	Status() model.StreamStatus
	UpdateProject(project model.ProjectState)
}

var rtmpStreamURLPattern = regexp.MustCompile(`rtmps?://[^\s]+/live2/[^\s]+`)

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
	mux.HandleFunc("/api/v1/runtime/stream-health", s.handleRuntimeStreamHealth)
	mux.HandleFunc("/api/v1/logs", s.handleLogs)
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
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			s.logger.Printf("request method=%s path=%s status=%d duration=%s remote=%s ua=%q", r.Method, r.URL.Path, http.StatusNoContent, "0s", requestSource(r), r.UserAgent())
			return
		}

		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		s.logger.Printf(
			"request method=%s path=%s status=%d duration=%s remote=%s ua=%q",
			r.Method,
			r.URL.Path,
			recorder.status,
			time.Since(started).String(),
			requestSource(r),
			r.UserAgent(),
		)
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
			for i := range state.OutputPresets {
				if state.OutputPresets[i].Settings.AudioSourceID == id {
					state.OutputPresets[i].Settings.AudioSourceID = ""
				}
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

func (s *Server) handleRuntimeStreamHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	streamStatus := s.engine.Status()
	health := runtimeStreamHealth{
		Running:           streamStatus.Running,
		StreamMode:        streamStatus.Mode,
		AnalysisLineLimit: 400,
		CriticalEvents:    []string{},
		WarningEvents:     []string{},
	}

	ffmpegLogPath, err := s.logPathByTarget("ffmpeg")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	info, err := os.Stat(ffmpegLogPath)
	if err != nil {
		if os.IsNotExist(err) {
			health.Status = "ok"
			if !streamStatus.Running {
				health.Status = "idle"
				health.Message = "stream is not running"
			} else {
				health.Message = "no ffmpeg log file yet"
			}
			writeJSON(w, http.StatusOK, health)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	updatedAt := info.ModTime().UTC()
	health.LogUpdatedAt = &updatedAt

	lines, _, err := readLogTail(ffmpegLogPath, health.AnalysisLineLimit, 512*1024)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	analyzeFFmpegHealth(s.redactLogLines(lines), &health)
	writeJSON(w, http.StatusOK, health)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	target := strings.TrimSpace(r.URL.Query().Get("target"))
	if target == "" {
		target = "server"
	}

	lines := 200
	if rawLines := strings.TrimSpace(r.URL.Query().Get("lines")); rawLines != "" {
		parsed, err := strconv.Atoi(rawLines)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid lines"))
			return
		}
		lines = parsed
	}
	if lines < 1 || lines > 2000 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("lines must be between 1 and 2000"))
		return
	}

	path, err := s.logPathByTarget(target)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("log file not found"))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	linesPayload, truncated, err := readLogTail(path, lines, 512*1024)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	linesPayload = s.redactLogLines(linesPayload)

	writeJSON(w, http.StatusOK, map[string]any{
		"target":     target,
		"lines":      linesPayload,
		"truncated":  truncated,
		"updatedAt":  info.ModTime().UTC(),
		"lineLimit":  lines,
		"byteWindow": 512 * 1024,
	})
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

func (s *Server) logPathByTarget(target string) (string, error) {
	switch target {
	case "server":
		return filepath.Join(s.dataDir, "logs", "server.log"), nil
	case "ffmpeg":
		return filepath.Join(s.dataDir, "logs", "ffmpeg.log"), nil
	default:
		return "", fmt.Errorf("unsupported log target: %s", target)
	}
}

func (s *Server) redactLogLines(lines []string) []string {
	project, err := s.store.Load()
	streamKey := ""
	if err == nil {
		streamKey = strings.TrimSpace(project.Output.YouTube.StreamKey)
	}

	redacted := make([]string, 0, len(lines))
	for _, line := range lines {
		redacted = append(redacted, redactLogLine(line, streamKey))
	}
	return redacted
}

func redactLogLine(line string, streamKey string) string {
	if streamKey != "" {
		line = strings.ReplaceAll(line, streamKey, "[REDACTED_STREAM_KEY]")
	}
	return rtmpStreamURLPattern.ReplaceAllString(line, "rtmp://[REDACTED]/live2/[REDACTED_STREAM_KEY]")
}

func readLogTail(path string, lineLimit int, maxBytes int64) ([]string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}

	size := info.Size()
	start := int64(0)
	if size > maxBytes {
		start = size - maxBytes
	}

	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, false, err
	}

	body, err := io.ReadAll(file)
	if err != nil {
		return nil, false, err
	}

	if start > 0 {
		if newline := bytes.IndexByte(body, '\n'); newline >= 0 && newline+1 < len(body) {
			body = body[newline+1:]
		}
	}

	rawLines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	filtered := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, line)
	}

	truncated := start > 0
	if len(filtered) > lineLimit {
		filtered = filtered[len(filtered)-lineLimit:]
		truncated = true
	}

	return filtered, truncated, nil
}

func analyzeFFmpegHealth(lines []string, health *runtimeStreamHealth) {
	normalized := normalizeLogLines(lines)
	criticalPatterns := []string{
		"broken pipe",
		"error writing trailer",
		"error closing file",
		"conversion failed",
	}
	warningPatterns := []string{
		"connection refused",
		"failed to reload playlist",
		"http error",
		"thread message queue blocking",
	}

	for _, line := range normalized {
		lower := strings.ToLower(line)
		if containsAny(lower, criticalPatterns) {
			health.CriticalEvents = append(health.CriticalEvents, line)
			continue
		}
		if containsAny(lower, warningPatterns) {
			health.WarningEvents = append(health.WarningEvents, line)
		}
	}

	health.CriticalEvents = tailLines(health.CriticalEvents, 20)
	health.WarningEvents = tailLines(health.WarningEvents, 20)
	health.CriticalEventCount = len(health.CriticalEvents)
	health.WarningEventCount = len(health.WarningEvents)

	switch {
	case !health.Running:
		health.Status = "idle"
		health.Message = "stream is not running"
	case health.CriticalEventCount > 0:
		health.Status = "error"
		health.Message = "ffmpeg critical events detected"
	case health.WarningEventCount > 0:
		health.Status = "warning"
		health.Message = "ffmpeg warning events detected"
	default:
		health.Status = "ok"
		health.Message = "no ffmpeg warning/error signature detected"
	}
}

func normalizeLogLines(lines []string) []string {
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		parts := strings.FieldsFunc(strings.ReplaceAll(line, "\r\n", "\n"), func(r rune) bool {
			return r == '\n' || r == '\r'
		})
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}

func containsAny(line string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(line, pattern) {
			return true
		}
	}
	return false
}

func tailLines(lines []string, limit int) []string {
	if len(lines) <= limit {
		return lines
	}
	return lines[len(lines)-limit:]
}

func (s *Server) handleStreamStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	s.logger.Printf("stream start requested remote=%s ua=%q", requestSource(r), r.UserAgent())

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
	s.persistStreamRunState(true)
	writeJSON(w, http.StatusOK, model.StateResponse{Project: project, Stream: status})
}

func (s *Server) handleStreamStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	s.logger.Printf("stream stop requested via api remote=%s ua=%q", requestSource(r), r.UserAgent())

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
	s.persistStreamRunState(false)
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
	if err := validateOutputSettings(dataDir, project.Output, hlsSourceIDs); err != nil {
		return err
	}

	seenPresetIDs := make(map[string]struct{}, len(project.OutputPresets))
	for _, preset := range project.OutputPresets {
		if strings.TrimSpace(preset.ID) == "" {
			return fmt.Errorf("output preset id is required")
		}
		if strings.TrimSpace(preset.Name) == "" {
			return fmt.Errorf("output preset name is required")
		}
		if _, exists := seenPresetIDs[preset.ID]; exists {
			return fmt.Errorf("duplicate output preset id %s", preset.ID)
		}
		seenPresetIDs[preset.ID] = struct{}{}
		if err := validateOutputSettings(dataDir, preset.Settings, hlsSourceIDs); err != nil {
			return fmt.Errorf("invalid output preset %s: %w", preset.ID, err)
		}
	}
	if project.ActiveOutputPresetID != "" {
		if _, ok := seenPresetIDs[project.ActiveOutputPresetID]; !ok {
			return fmt.Errorf("activeOutputPresetId %s does not reference an output preset", project.ActiveOutputPresetID)
		}
	}
	return nil
}

func validateOutputSettings(dataDir string, output model.OutputSettings, hlsSourceIDs map[string]struct{}) error {
	switch output.Mode {
	case model.OutputModeHLS:
		if _, err := stream.ResolveOutputPath(dataDir, output.HLS.Path); err != nil {
			return fmt.Errorf("invalid hls output path: %w", err)
		}
		if err := validateHLSPublicPath(output.HLS.PublicPath); err != nil {
			return fmt.Errorf("invalid hls public path: %w", err)
		}
	case model.OutputModeYouTube:
	default:
		return fmt.Errorf("unsupported output mode %q", output.Mode)
	}
	if output.AudioSourceID != "" {
		if _, ok := hlsSourceIDs[output.AudioSourceID]; !ok {
			return fmt.Errorf("audioSourceId %s does not reference an HLS source", output.AudioSourceID)
		}
	}
	return nil
}

func validateHLSPublicPath(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("public path is required")
	}
	if strings.HasPrefix(trimmed, "//") {
		return fmt.Errorf("public path must be a local path")
	}

	parsed, err := neturl.Parse(trimmed)
	if err != nil {
		return err
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		return fmt.Errorf("public path must not include scheme or host")
	}
	if !strings.HasPrefix(parsed.Path, "/") {
		return fmt.Errorf("public path must start with '/'")
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

func (s *Server) streamRunStatePath() string {
	return filepath.Join(s.dataDir, "runtime", "stream-run-state.json")
}

func (s *Server) persistStreamRunState(shouldRun bool) {
	path := s.streamRunStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.logger.Printf("failed to create runtime dir for stream run state: %v", err)
		return
	}
	payload := streamRunState{
		ShouldRun: shouldRun,
		UpdatedAt: time.Now().UTC(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		s.logger.Printf("failed to marshal stream run state: %v", err)
		return
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		s.logger.Printf("failed to write stream run state: %v", err)
	}
}

func requestSource(r *http.Request) string {
	forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
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
	s.logger.Printf("stream stop requested for sync reload mode=%s", status.Mode)

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
