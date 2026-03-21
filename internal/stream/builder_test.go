package stream

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"streaming-studio/internal/model"
)

func TestBuildFFmpegArgsForHLSOutput(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	project := model.DefaultProjectState()
	project.Assets = append(project.Assets, model.Asset{
		ID:        "img-1",
		Kind:      model.AssetKindImage,
		Name:      "Overlay",
		FileName:  "overlay.png",
		Path:      "assets/images/overlay.png",
		URL:       "/uploads/images/overlay.png",
		CreatedAt: time.Now(),
	})
	project.Sources = []model.Source{
		{
			ID:      "cam-a",
			Name:    "Camera A",
			Kind:    model.SourceKindHLS,
			Enabled: true,
			Layout: model.Layout{
				X:       0,
				Y:       0,
				Width:   1280,
				Height:  720,
				Opacity: 1,
				ZIndex:  0,
			},
			HLS: &model.HLSSource{URL: "https://example.com/live-a.m3u8"},
		},
		{
			ID:      "overlay-1",
			Name:    "Overlay",
			Kind:    model.SourceKindImage,
			Enabled: true,
			Layout: model.Layout{
				X:       32,
				Y:       48,
				Width:   320,
				Height:  180,
				Radius:  18,
				Opacity: 0.75,
				ZIndex:  5,
			},
			Image: &model.ImageSource{AssetID: "img-1"},
		},
		{
			ID:      "title-1",
			Name:    "Title",
			Kind:    model.SourceKindText,
			Enabled: true,
			Layout: model.Layout{
				X:       64,
				Y:       640,
				Width:   400,
				Height:  50,
				Opacity: 1,
				ZIndex:  10,
			},
			Text: &model.TextSource{
				Content:         "Hello World",
				FontSize:        42,
				Color:           "#ffffff",
				BackgroundColor: "#000000",
			},
		},
	}

	result, err := BuildFFmpegArgs(project, BuildConfig{DataDir: dataDir})
	if err != nil {
		t.Fatalf("BuildFFmpegArgs() returned error: %v", err)
	}

	command := strings.Join(result.Args, " ")
	assertContains(t, command, "https://example.com/live-a.m3u8")
	assertContains(t, command, "-reconnect 1")
	assertContains(t, command, "-reconnect_streamed 1")
	assertContains(t, command, "-reconnect_on_network_error 1")
	assertContains(t, command, "-reconnect_on_http_error 4xx,5xx")
	assertContains(t, command, "-reconnect_delay_max 10")
	assertContains(t, command, "-rw_timeout 15000000")
	assertContains(t, command, filepath.Join(dataDir, "assets/images/overlay.png"))
	assertContains(t, command, "textfile='")
	assertContains(t, command, "boxcolor=#000000@0.800")
	assertContains(t, command, "-f hls")
	assertContains(t, command, filepath.Join(dataDir, "output/hls/live.m3u8"))
	assertContains(t, command, "colorchannelmixer=aa=0.750")
	assertContains(t, command, "geq=r='r(X,Y)'")

	textFile := filepath.Join(dataDir, "runtime", "text", "title-1.txt")
	content, err := os.ReadFile(textFile)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) returned error: %v", textFile, err)
	}
	if string(content) != "Hello World" {
		t.Fatalf("text file content = %q, want Hello World", string(content))
	}
}

func TestBuildFFmpegArgsForYouTubePreset(t *testing.T) {
	t.Parallel()

	project := model.DefaultProjectState()
	project.Output.Mode = model.OutputModeYouTube
	project.Output.YouTube.StreamKey = "abcd-efgh-ijkl"
	project.Output.VideoBitrate = "6000k"
	project.Sources = []model.Source{
		{
			ID:      "cam-a",
			Name:    "Camera A",
			Kind:    model.SourceKindHLS,
			Enabled: true,
			Layout: model.Layout{
				Width:   1280,
				Height:  720,
				Opacity: 1,
			},
			HLS: &model.HLSSource{URL: "https://example.com/live-a.m3u8"},
		},
	}

	result, err := BuildFFmpegArgs(project, BuildConfig{DataDir: "/var/lib/studio"})
	if err != nil {
		t.Fatalf("BuildFFmpegArgs() returned error: %v", err)
	}

	command := strings.Join(result.Args, " ")
	assertContains(t, command, "-f flv rtmp://a.rtmp.youtube.com/live2/abcd-efgh-ijkl")
	assertContains(t, command, "-maxrate 6000k -bufsize 12000k -tune zerolatency")
}

func TestBuildFFmpegArgsRejectsUnsafeHLSOutputPath(t *testing.T) {
	t.Parallel()

	project := model.DefaultProjectState()
	project.Output.HLS.Path = "../../../tmp/live.m3u8"
	project.Sources = []model.Source{
		{
			ID:      "cam-a",
			Name:    "Camera A",
			Kind:    model.SourceKindHLS,
			Enabled: true,
			Layout: model.Layout{
				Width:   1280,
				Height:  720,
				Opacity: 1,
			},
			HLS: &model.HLSSource{URL: "https://example.com/live-a.m3u8"},
		},
	}

	if _, err := BuildFFmpegArgs(project, BuildConfig{DataDir: "/data"}); err == nil {
		t.Fatalf("BuildFFmpegArgs() error = nil, want invalid output path error")
	}
}

func TestBuildFFmpegArgsKeepsOpacityZeroAndFallsBackAudio(t *testing.T) {
	t.Parallel()

	project := model.DefaultProjectState()
	project.Output.AudioSourceID = "missing-source"
	project.Sources = []model.Source{
		{
			ID:      "cam-a",
			Name:    "Camera A",
			Kind:    model.SourceKindHLS,
			Enabled: true,
			Layout: model.Layout{
				Width:   1280,
				Height:  720,
				Opacity: 1,
			},
			HLS: &model.HLSSource{URL: "https://example.com/live-a.m3u8"},
		},
		{
			ID:      "overlay-1",
			Name:    "Overlay",
			Kind:    model.SourceKindImage,
			Enabled: true,
			Layout: model.Layout{
				X:       10,
				Y:       20,
				Width:   320,
				Height:  180,
				Opacity: 0,
				ZIndex:  1,
			},
			Image: &model.ImageSource{AssetID: "img-1"},
		},
	}
	project.Assets = []model.Asset{
		{
			ID:   "img-1",
			Kind: model.AssetKindImage,
			Path: "assets/images/overlay.png",
		},
	}

	result, err := BuildFFmpegArgs(project, BuildConfig{DataDir: "/data"})
	if err != nil {
		t.Fatalf("BuildFFmpegArgs() returned error: %v", err)
	}

	command := strings.Join(result.Args, " ")
	assertContains(t, command, "colorchannelmixer=aa=0.000")
	assertContains(t, command, "-map 1:a?")
}

func TestBuildFFmpegArgsUsesNotoFallbackForJapaneseText(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	project := model.DefaultProjectState()
	project.Sources = []model.Source{
		{
			ID:      "title-ja",
			Name:    "Japanese Title",
			Kind:    model.SourceKindText,
			Enabled: true,
			Layout: model.Layout{
				X:       40,
				Y:       60,
				Width:   500,
				Height:  80,
				Opacity: 1,
				ZIndex:  0,
			},
			Text: &model.TextSource{
				Content:  "日本語テキスト",
				FontSize: 42,
				Color:    "#ffffff",
			},
		},
	}

	result, err := BuildFFmpegArgs(project, BuildConfig{DataDir: dataDir})
	if err != nil {
		t.Fatalf("BuildFFmpegArgs() returned error: %v", err)
	}

	command := strings.Join(result.Args, " ")
	assertContains(t, command, "textfile='")
	assertContains(t, command, "fontfile='"+escapeFFmpegPath(defaultDrawtextFontPath)+"'")
}

func TestBuildFFmpegArgsWritesMultilineTextFile(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	project := model.DefaultProjectState()
	project.Sources = []model.Source{
		{
			ID:      "title-multi",
			Name:    "Multiline Title",
			Kind:    model.SourceKindText,
			Enabled: true,
			Layout: model.Layout{
				X:       20,
				Y:       30,
				Width:   400,
				Height:  100,
				Opacity: 1,
				ZIndex:  0,
			},
			Text: &model.TextSource{
				Content:     "1行目\n2行目",
				FontSize:    32,
				LineSpacing: 12,
				Color:       "#ffffff",
			},
		},
	}

	result, err := BuildFFmpegArgs(project, BuildConfig{DataDir: dataDir})
	if err != nil {
		t.Fatalf("BuildFFmpegArgs() returned error: %v", err)
	}

	command := strings.Join(result.Args, " ")
	textFile := filepath.Join(dataDir, "runtime", "text", "title-multi.txt")
	assertContains(t, command, "textfile='"+escapeFFmpegPath(textFile)+"'")
	assertContains(t, command, ":reload=1")

	content, err := os.ReadFile(textFile)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) returned error: %v", textFile, err)
	}
	if string(content) != "1行目\n2行目" {
		t.Fatalf("text file content = %q, want %q", string(content), "1行目\n2行目")
	}
}

func TestBuildFFmpegArgsWrapsLongTextToSourceWidth(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	project := model.DefaultProjectState()
	project.Sources = []model.Source{
		{
			ID:      "title-wrap",
			Name:    "Wrapped Title",
			Kind:    model.SourceKindText,
			Enabled: true,
			Layout: model.Layout{
				X:       10,
				Y:       20,
				Width:   180,
				Height:  120,
				Opacity: 1,
				ZIndex:  0,
			},
			Text: &model.TextSource{
				Content:  "ABCDEFGHIJ",
				FontSize: 40,
				Color:    "#ffffff",
			},
		},
	}

	if _, err := BuildFFmpegArgs(project, BuildConfig{DataDir: dataDir}); err != nil {
		t.Fatalf("BuildFFmpegArgs() returned error: %v", err)
	}

	textFile := filepath.Join(dataDir, "runtime", "text", "title-wrap.txt")
	content, err := os.ReadFile(textFile)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) returned error: %v", textFile, err)
	}
	if !strings.Contains(string(content), "\n") {
		t.Fatalf("text file content = %q, want wrapped text with newline", string(content))
	}
}

func TestBuildFFmpegArgsOmitsTextBackgroundBoxWhenOpacityIsZero(t *testing.T) {
	t.Parallel()

	backgroundOpacity := 0.0
	project := model.DefaultProjectState()
	project.Sources = []model.Source{
		{
			ID:      "title-transparent-bg",
			Name:    "Transparent Background",
			Kind:    model.SourceKindText,
			Enabled: true,
			Layout: model.Layout{
				X:       20,
				Y:       30,
				Width:   400,
				Height:  100,
				Opacity: 1,
				ZIndex:  0,
			},
			Text: &model.TextSource{
				Content:           "No box",
				FontSize:          32,
				Color:             "#ffffff",
				BackgroundColor:   "#111827",
				BackgroundOpacity: &backgroundOpacity,
			},
		},
	}

	result, err := BuildFFmpegArgs(project, BuildConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("BuildFFmpegArgs() returned error: %v", err)
	}

	command := strings.Join(result.Args, " ")
	if strings.Contains(command, ":box=1:boxcolor=") {
		t.Fatalf("command = %q, want no drawtext box when background opacity is zero", command)
	}
}

func TestBuildFFmpegArgsUsesRoundedBackgroundOverlayForTextRadius(t *testing.T) {
	t.Parallel()

	backgroundOpacity := 0.8
	project := model.DefaultProjectState()
	project.Sources = []model.Source{
		{
			ID:      "title-radius",
			Name:    "Rounded Text",
			Kind:    model.SourceKindText,
			Enabled: true,
			Layout: model.Layout{
				X:       20,
				Y:       30,
				Width:   400,
				Height:  100,
				Radius:  16,
				Opacity: 1,
				ZIndex:  0,
			},
			Text: &model.TextSource{
				Content:           "Rounded",
				FontSize:          32,
				Color:             "#ffffff",
				BackgroundColor:   "#111827",
				BackgroundOpacity: &backgroundOpacity,
			},
		},
	}

	result, err := BuildFFmpegArgs(project, BuildConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("BuildFFmpegArgs() returned error: %v", err)
	}

	command := strings.Join(result.Args, " ")
	assertContains(t, command, "color=c=#111827@0.800:s=400x100")
	assertContains(t, command, "overlay=20:30:format=auto")
	if strings.Contains(command, ":box=1:boxcolor=") {
		t.Fatalf("command = %q, want rounded text background to use overlay instead of drawtext box", command)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected %q to contain %q", haystack, needle)
	}
}
