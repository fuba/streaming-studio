package stream

import (
	"fmt"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"streaming-studio/internal/model"
	"streaming-studio/internal/store"
)

const defaultRemoteTextRefreshInterval = 5 * time.Second

type TextRefresher struct {
	store   *store.FileStore
	dataDir string
	logger  *log.Logger
	client  *http.Client

	mu       sync.Mutex
	nextPoll map[string]time.Time
	configs  map[string]string
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func NewTextRefresher(stateStore *store.FileStore, dataDir string, logger *log.Logger) *TextRefresher {
	return &TextRefresher{
		store:   stateStore,
		dataDir: dataDir,
		logger:  logger,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		nextPoll: make(map[string]time.Time),
		configs:  make(map[string]string),
	}
}

func (r *TextRefresher) Start() {
	r.mu.Lock()
	if r.stopCh != nil {
		r.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	r.stopCh = stopCh
	r.doneCh = doneCh
	r.mu.Unlock()

	go r.loop(stopCh, doneCh)
}

func (r *TextRefresher) Stop() {
	r.mu.Lock()
	stopCh := r.stopCh
	doneCh := r.doneCh
	if stopCh == nil {
		r.mu.Unlock()
		return
	}
	r.stopCh = nil
	r.doneCh = nil
	r.mu.Unlock()

	close(stopCh)
	<-doneCh
}

func (r *TextRefresher) loop(stopCh, doneCh chan struct{}) {
	defer close(doneCh)
	r.syncOnce(time.Now().UTC())

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.syncOnce(time.Now().UTC())
		case <-stopCh:
			return
		}
	}
}

func (r *TextRefresher) syncOnce(now time.Time) {
	project, err := r.store.Load()
	if err != nil {
		r.logger.Printf("remote text poll skipped: %v", err)
		return
	}

	active := make(map[string]struct{})

	for _, source := range project.Sources {
		if !source.Enabled || source.Kind != model.SourceKindText || source.Text == nil || source.Text.Remote == nil {
			continue
		}
		remoteURL := strings.TrimSpace(source.Text.Remote.URL)
		if remoteURL == "" {
			continue
		}

		active[source.ID] = struct{}{}
		signature := remoteSourceSignature(source)

		r.mu.Lock()
		nextPoll, hasNextPoll := r.nextPoll[source.ID]
		configChanged := r.configs[source.ID] != signature
		r.mu.Unlock()

		if hasNextPoll && now.Before(nextPoll) && !configChanged {
			continue
		}

		if err := r.fetchAndWrite(source); err != nil {
			r.logger.Printf("remote text poll failed for %s: %v", source.ID, err)
			if _, fallbackErr := prepareDrawtextFile(r.dataDir, source.ID, wrapTextForSource(source, source.Text.Content)); fallbackErr != nil {
				r.logger.Printf("remote text fallback write failed for %s: %v", source.ID, fallbackErr)
			}
		}

		r.mu.Lock()
		r.nextPoll[source.ID] = now.Add(refreshInterval(source.Text.Remote))
		r.configs[source.ID] = signature
		r.mu.Unlock()
	}

	r.mu.Lock()
	for sourceID := range r.nextPoll {
		if _, ok := active[sourceID]; !ok {
			delete(r.nextPoll, sourceID)
			delete(r.configs, sourceID)
		}
	}
	r.mu.Unlock()
}

func (r *TextRefresher) fetchAndWrite(source model.Source) error {
	remoteURL := strings.TrimSpace(source.Text.Remote.URL)
	if _, err := neturl.ParseRequestURI(remoteURL); err != nil {
		return err
	}

	response, err := r.client.Get(remoteURL)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected status %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}

	_, err = prepareDrawtextFile(r.dataDir, source.ID, wrapTextForSource(source, normalizeRemoteText(string(body))))
	return err
}

func normalizeRemoteText(input string) string {
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.TrimRight(normalized, "\n")
}

func refreshInterval(remote *model.RemoteTextSource) time.Duration {
	if remote == nil || remote.RefreshIntervalSeconds <= 0 {
		return defaultRemoteTextRefreshInterval
	}
	return time.Duration(remote.RefreshIntervalSeconds) * time.Second
}

func remoteSourceSignature(source model.Source) string {
	if source.Text == nil || source.Text.Remote == nil {
		return ""
	}

	parts := []string{
		strings.TrimSpace(source.Text.Remote.URL),
		refreshInterval(source.Text.Remote).String(),
		strconv.Itoa(source.Layout.Width),
		strconv.Itoa(source.Text.FontSize),
		source.Text.Content,
	}
	return strings.Join(parts, "\x00")
}
