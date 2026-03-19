package stream

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"streaming-studio/internal/model"
	"streaming-studio/internal/store"
)

func TestTextRefresherSyncOnceWritesRemoteTextFile(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	project := model.DefaultProjectState()
	project.Sources = []model.Source{
		{
			ID:      "remote-text-1",
			Name:    "Remote Text",
			Kind:    model.SourceKindText,
			Enabled: true,
			Layout: model.Layout{
				Width:   400,
				Height:  120,
				Opacity: 1,
			},
			Text: &model.TextSource{
				Content: "fallback",
				Remote: &model.RemoteTextSource{
					RefreshIntervalSeconds: 1,
				},
			},
		},
	}

	var mu sync.Mutex
	remoteContent := "1行目\r\n2行目\r\n"
	refresher := NewTextRefresher(stateStore, dataDir, log.New(bytes.NewBuffer(nil), "", 0))
	refresher.client = &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			mu.Lock()
			defer mu.Unlock()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(remoteContent)),
				Header:     make(http.Header),
				Request:    request,
			}, nil
		}),
	}

	project.Sources[0].Text.Remote.URL = "http://remote.test/info.txt"
	if err := stateStore.Save(project); err != nil {
		t.Fatalf("stateStore.Save() returned error: %v", err)
	}

	now := time.Now().UTC()
	refresher.syncOnce(now)

	assertTextFileContent(t, dataDir, "remote-text-1", "1行目\n2行目")

	mu.Lock()
	remoteContent = "更新済み"
	mu.Unlock()

	refresher.syncOnce(now.Add(2 * time.Second))

	assertTextFileContent(t, dataDir, "remote-text-1", "更新済み")
}

func TestTextRefresherSyncOnceWrapsRemoteTextToSourceWidth(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	project := model.DefaultProjectState()
	project.Sources = []model.Source{
		{
			ID:      "remote-text-wrap",
			Name:    "Remote Text Wrap",
			Kind:    model.SourceKindText,
			Enabled: true,
			Layout: model.Layout{
				Width:   180,
				Height:  120,
				Opacity: 1,
			},
			Text: &model.TextSource{
				Content:  "fallback",
				FontSize: 40,
				Remote: &model.RemoteTextSource{
					RefreshIntervalSeconds: 1,
				},
			},
		},
	}

	refresher := NewTextRefresher(stateStore, dataDir, log.New(bytes.NewBuffer(nil), "", 0))
	refresher.client = &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("あいうえおかき")),
				Header:     make(http.Header),
				Request:    request,
			}, nil
		}),
	}

	project.Sources[0].Text.Remote.URL = "http://remote.test/info.txt"
	if err := stateStore.Save(project); err != nil {
		t.Fatalf("stateStore.Save() returned error: %v", err)
	}

	refresher.syncOnce(time.Now().UTC())

	assertTextFileContent(t, dataDir, "remote-text-wrap", "あいうえ\nおかき")
}

func TestTextRefresherSyncOnceRewrapsWhenSourceSettingsChange(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	stateStore := store.NewFileStore(filepath.Join(dataDir, "state.json"))
	project := model.DefaultProjectState()
	project.Sources = []model.Source{
		{
			ID:      "remote-text-config",
			Name:    "Remote Text Config",
			Kind:    model.SourceKindText,
			Enabled: true,
			Layout: model.Layout{
				Width:   180,
				Height:  120,
				Opacity: 1,
			},
			Text: &model.TextSource{
				Content:  "fallback",
				FontSize: 40,
				Remote: &model.RemoteTextSource{
					URL:                    "http://remote.test/info.txt",
					RefreshIntervalSeconds: 60,
				},
			},
		},
	}

	refresher := NewTextRefresher(stateStore, dataDir, log.New(bytes.NewBuffer(nil), "", 0))
	refresher.client = &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("あいうえおかき")),
				Header:     make(http.Header),
				Request:    request,
			}, nil
		}),
	}

	if err := stateStore.Save(project); err != nil {
		t.Fatalf("stateStore.Save() returned error: %v", err)
	}

	now := time.Now().UTC()
	refresher.syncOnce(now)
	assertTextFileContent(t, dataDir, "remote-text-config", "あいうえ\nおかき")

	project.Sources[0].Layout.Width = 120
	if err := stateStore.Save(project); err != nil {
		t.Fatalf("stateStore.Save() after width change returned error: %v", err)
	}

	refresher.syncOnce(now.Add(5 * time.Second))
	assertTextFileContent(t, dataDir, "remote-text-config", "あいう\nえおか\nき")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func assertTextFileContent(t *testing.T, dataDir, sourceID, want string) {
	t.Helper()

	path := filepath.Join(dataDir, "runtime", "text", sourceID+".txt")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) returned error: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("text file content = %q, want %q", string(got), want)
	}
}
