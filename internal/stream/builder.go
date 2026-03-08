package stream

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"streaming-studio/internal/model"
)

type BuildConfig struct {
	DataDir string
}

type BuildResult struct {
	Args []string
}

const defaultDrawtextFontPath = "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"

func BuildFFmpegArgs(project model.ProjectState, cfg BuildConfig) (BuildResult, error) {
	if project.Canvas.Width <= 0 || project.Canvas.Height <= 0 {
		return BuildResult{}, fmt.Errorf("canvas size must be positive")
	}

	enabled := make([]model.Source, 0, len(project.Sources))
	for _, source := range project.Sources {
		if source.Enabled {
			enabled = append(enabled, source)
		}
	}

	slices.SortFunc(enabled, func(a, b model.Source) int {
		if a.Layout.ZIndex == b.Layout.ZIndex {
			return strings.Compare(a.ID, b.ID)
		}
		return a.Layout.ZIndex - b.Layout.ZIndex
	})

	args := []string{
		"-hide_banner",
		"-y",
		"-f", "lavfi",
		"-i", fmt.Sprintf("color=c=%s:s=%dx%d:r=%d", ffmpegColor(project.Canvas.BackgroundColor), project.Canvas.Width, project.Canvas.Height, project.Output.FrameRate),
	}

	inputIndexes := make(map[string]int)
	nextInput := 1

	for _, source := range enabled {
		inputIndexes[source.ID] = nextInput
		switch source.Kind {
		case model.SourceKindHLS:
			if source.HLS == nil || source.HLS.URL == "" {
				return BuildResult{}, fmt.Errorf("source %s is missing HLS URL", source.ID)
			}
			args = append(args, "-fflags", "+genpts", "-i", source.HLS.URL)
		case model.SourceKindImage:
			if source.Image == nil || source.Image.AssetID == "" {
				return BuildResult{}, fmt.Errorf("source %s is missing image asset", source.ID)
			}
			assetPath, err := assetPathByID(project.Assets, cfg.DataDir, source.Image.AssetID)
			if err != nil {
				return BuildResult{}, err
			}
			args = append(args, "-loop", "1", "-i", assetPath)
		case model.SourceKindText:
		default:
			return BuildResult{}, fmt.Errorf("unsupported source kind %q", source.Kind)
		}
		if source.Kind != model.SourceKindText {
			nextInput++
		}
	}

	filterParts := []string{"[0:v]format=rgba[stage0]"}
	stageLabel := "stage0"
	stageIndex := 1

	for _, source := range enabled {
		switch source.Kind {
		case model.SourceKindHLS, model.SourceKindImage:
			inputLabel := fmt.Sprintf("[%d:v]", inputIndexes[source.ID])
			currentSourceLabel := fmt.Sprintf("src%d", stageIndex)
			videoFilter := fmt.Sprintf("%sscale=%d:%d:flags=lanczos,format=rgba", inputLabel, source.Layout.Width, source.Layout.Height)
			if source.Kind == model.SourceKindHLS {
				videoFilter = fmt.Sprintf("%ssetpts=PTS-STARTPTS,scale=%d:%d:flags=lanczos,format=rgba", inputLabel, source.Layout.Width, source.Layout.Height)
			}
			if radius := effectiveRadius(source.Layout.Width, source.Layout.Height, source.Layout.Radius); radius > 0 {
				videoFilter += roundedCornersFilter(source.Layout.Width, source.Layout.Height, radius)
			}
			if source.Layout.Rotation != 0 {
				videoFilter += fmt.Sprintf(",rotate=%s:c=none:ow=rotw(iw):oh=roth(ih)", strconv.FormatFloat(source.Layout.Rotation, 'f', -1, 64))
			}
			if source.Layout.Opacity < 1 {
				videoFilter += fmt.Sprintf(",colorchannelmixer=aa=%s", strconv.FormatFloat(clampOpacity(source.Layout.Opacity), 'f', 3, 64))
			}
			videoFilter += fmt.Sprintf("[%s]", currentSourceLabel)
			filterParts = append(filterParts, videoFilter)

			nextStage := fmt.Sprintf("stage%d", stageIndex)
			filterParts = append(filterParts, fmt.Sprintf("[%s][%s]overlay=%d:%d:format=auto[%s]", stageLabel, currentSourceLabel, source.Layout.X, source.Layout.Y, nextStage))
			stageLabel = nextStage
			stageIndex++
		case model.SourceKindText:
			if source.Text == nil {
				return BuildResult{}, fmt.Errorf("source %s is missing text payload", source.ID)
			}
			textFilters, nextStage, err := buildTextFilters(stageLabel, source, project.Assets, cfg.DataDir, stageIndex)
			if err != nil {
				return BuildResult{}, err
			}
			filterParts = append(filterParts, textFilters...)
			stageLabel = nextStage
			stageIndex++
		}
	}

	filterParts = append(filterParts, fmt.Sprintf("[%s]format=yuv420p[vout]", stageLabel))
	args = append(args, "-filter_complex", strings.Join(filterParts, ";"), "-map", "[vout]")

	audioMapped := false
	if audioIndex, ok := resolveAudioInput(project, enabled, inputIndexes); ok {
		args = append(args, "-map", fmt.Sprintf("%d:a?", audioIndex), "-c:a", "aac", "-b:a", project.Output.AudioBitrate)
		audioMapped = true
	}
	if !audioMapped {
		args = append(args, "-an")
	}

	args = append(args,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-pix_fmt", "yuv420p",
		"-r", strconv.Itoa(project.Output.FrameRate),
		"-g", strconv.Itoa(project.Output.FrameRate*2),
		"-b:v", project.Output.VideoBitrate,
	)

	args = append(args, project.Output.AdditionalArgs...)

	switch project.Output.Mode {
	case model.OutputModeHLS:
		target, err := ResolveOutputPath(cfg.DataDir, project.Output.HLS.Path)
		if err != nil {
			return BuildResult{}, err
		}
		segmentPattern := filepath.Join(filepath.Dir(target), "segment_%06d.ts")
		args = append(args,
			"-f", "hls",
			"-hls_time", strconv.Itoa(project.Output.HLS.SegmentDuration),
			"-hls_list_size", strconv.Itoa(project.Output.HLS.ListSize),
			"-hls_flags", "delete_segments+append_list+omit_endlist+independent_segments",
			"-hls_segment_filename", segmentPattern,
			target,
		)
	case model.OutputModeYouTube:
		if project.Output.YouTube.StreamKey == "" {
			return BuildResult{}, fmt.Errorf("youtube stream key is required")
		}
		if project.Output.YouTube.Preset == "youtube-default" {
			args = append(args, "-maxrate", project.Output.VideoBitrate, "-bufsize", doubleBitrate(project.Output.VideoBitrate), "-tune", "zerolatency")
		}
		args = append(args, project.Output.YouTube.AdditionalArgs...)
		args = append(args, "-f", "flv", strings.TrimRight(project.Output.YouTube.RTMPURL, "/")+"/"+project.Output.YouTube.StreamKey)
	default:
		return BuildResult{}, fmt.Errorf("unsupported output mode %q", project.Output.Mode)
	}

	return BuildResult{Args: args}, nil
}

func assetPathByID(assets []model.Asset, dataDir, assetID string) (string, error) {
	for _, asset := range assets {
		if asset.ID == assetID {
			if filepath.IsAbs(asset.Path) {
				return asset.Path, nil
			}
			return filepath.Join(dataDir, asset.Path), nil
		}
	}
	return "", fmt.Errorf("asset %s was not found", assetID)
}

func resolveAudioInput(project model.ProjectState, enabled []model.Source, inputIndexes map[string]int) (int, bool) {
	if project.Output.AudioSourceID != "" {
		if audioIndex, ok := inputIndexes[project.Output.AudioSourceID]; ok {
			return audioIndex, true
		}
	}
	for _, source := range enabled {
		if source.Kind == model.SourceKindHLS {
			return inputIndexes[source.ID], true
		}
	}
	return 0, false
}

func buildTextFilters(stageLabel string, source model.Source, assets []model.Asset, dataDir string, stageIndex int) ([]string, string, error) {
	text := source.Text
	fontFile := ""
	if text.FontAssetID != "" {
		if path, err := assetPathByID(assets, dataDir, text.FontAssetID); err == nil {
			fontFile = path
		}
	}
	if fontFile == "" {
		fontFile = defaultDrawtextFontPath
	}

	if text.FontSize <= 0 {
		text.FontSize = 42
	}
	textPath, err := prepareDrawtextFile(dataDir, source.ID, text.Content)
	if err != nil {
		return nil, "", err
	}

	filters := make([]string, 0, 2)
	currentStage := stageLabel
	backgroundOpacity := textBackgroundOpacity(text)
	if text.BackgroundColor != "" && backgroundOpacity > 0 && source.Layout.Width > 0 && source.Layout.Height > 0 {
		if radius := effectiveRadius(source.Layout.Width, source.Layout.Height, source.Layout.Radius); radius > 0 {
			backgroundLabel := fmt.Sprintf("textbg%d", stageIndex)
			filters = append(filters, fmt.Sprintf(
				"color=c=%s:s=%dx%d,format=rgba%s[%s]",
				ffmpegColorWithAlpha(text.BackgroundColor, backgroundOpacity),
				source.Layout.Width,
				source.Layout.Height,
				roundedCornersFilter(source.Layout.Width, source.Layout.Height, radius),
				backgroundLabel,
			))

			nextStage := fmt.Sprintf("stage%d", stageIndex)
			filters = append(filters, fmt.Sprintf("[%s][%s]overlay=%d:%d:format=auto[%s]", currentStage, backgroundLabel, source.Layout.X, source.Layout.Y, nextStage))
			currentStage = nextStage
		}
	}

	drawtext := fmt.Sprintf("[%s]drawtext=textfile='%s':reload=1:x=%d:y=%d:fontsize=%d:fontcolor=%s", currentStage, escapeFFmpegPath(textPath), source.Layout.X, source.Layout.Y, text.FontSize, ffmpegColorWithAlpha(text.Color, source.Layout.Opacity))
	drawtext += ":fontfile='" + escapeFFmpegPath(fontFile) + "'"
	if text.BackgroundColor != "" && backgroundOpacity > 0 && source.Layout.Radius <= 0 {
		drawtext += ":box=1:boxcolor=" + ffmpegColorWithAlpha(text.BackgroundColor, backgroundOpacity)
	}
	if text.BorderWidth > 0 {
		drawtext += fmt.Sprintf(":borderw=%d:bordercolor=%s", text.BorderWidth, ffmpegColorWithAlpha(text.BorderColor, 1))
	}
	if text.LineSpacing != 0 {
		drawtext += fmt.Sprintf(":line_spacing=%d", text.LineSpacing)
	}
	nextStage := fmt.Sprintf("stage%d", stageIndex)
	filters = append(filters, drawtext+"["+nextStage+"]")
	return filters, nextStage, nil
}

func ffmpegColor(input string) string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(input, "#"))
	if trimmed == "" {
		return "0x000000"
	}
	return "0x" + trimmed
}

func ffmpegColorWithAlpha(input string, alpha float64) string {
	color := strings.TrimSpace(strings.TrimPrefix(input, "#"))
	if color == "" {
		color = "ffffff"
	}
	alpha = clampOpacity(alpha)
	if alpha >= 1 {
		return "#" + color
	}
	return fmt.Sprintf("#%s@%s", color, strconv.FormatFloat(alpha, 'f', 3, 64))
}

func clampOpacity(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func textBackgroundOpacity(text *model.TextSource) float64 {
	if text == nil || text.BackgroundOpacity == nil {
		return 0.8
	}
	return clampOpacity(*text.BackgroundOpacity)
}

func effectiveRadius(width, height, radius int) int {
	if radius < 0 {
		return 0
	}
	limit := width
	if height < limit {
		limit = height
	}
	if limit <= 0 {
		return 0
	}
	maxRadius := limit / 2
	if radius > maxRadius {
		return maxRadius
	}
	return radius
}

func roundedCornersFilter(width, height, radius int) string {
	horizontalInset := width/2 - radius
	verticalInset := height/2 - radius
	alphaExpr := fmt.Sprintf(
		"if(lte(pow(max(abs(X-W/2)-(%d)\\,0)\\,2)+pow(max(abs(Y-H/2)-(%d)\\,0)\\,2)\\,%d)\\,255\\,0)",
		horizontalInset,
		verticalInset,
		radius*radius,
	)
	return fmt.Sprintf(",format=rgba,geq=r='r(X,Y)':g='g(X,Y)':b='b(X,Y)':a='%s'", alphaExpr)
}

func prepareDrawtextFile(dataDir, sourceID, content string) (string, error) {
	textDir := filepath.Join(dataDir, "runtime", "text")
	if err := os.MkdirAll(textDir, 0o755); err != nil {
		return "", err
	}

	targetPath := filepath.Join(textDir, sourceID+".txt")
	if err := writeTextFile(targetPath, content); err != nil {
		return "", err
	}
	return targetPath, nil
}

func escapeFFmpegText(input string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"'", "\\'",
		":", "\\:",
		"%", "\\%",
		"\n", "\\n",
	)
	return replacer.Replace(input)
}

func escapeFFmpegPath(input string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"'", "\\'",
		":", "\\:",
	)
	return replacer.Replace(input)
}

func doubleBitrate(value string) string {
	value = strings.TrimSpace(strings.TrimSuffix(value, "k"))
	if value == "" {
		return "9000k"
	}
	numeric, err := strconv.Atoi(value)
	if err != nil {
		return "9000k"
	}
	return strconv.Itoa(numeric*2) + "k"
}
