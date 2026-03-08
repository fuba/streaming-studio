package model

import "time"

type SourceKind string

const (
	SourceKindHLS   SourceKind = "hls"
	SourceKindImage SourceKind = "image"
	SourceKindText  SourceKind = "text"
)

type AssetKind string

const (
	AssetKindImage AssetKind = "image"
	AssetKindFont  AssetKind = "font"
)

type OutputMode string

const (
	OutputModeHLS     OutputMode = "hls"
	OutputModeYouTube OutputMode = "youtube"
)

type Canvas struct {
	Width                 int    `json:"width"`
	Height                int    `json:"height"`
	BackgroundColor       string `json:"backgroundColor"`
	EditorBackgroundColor string `json:"editorBackgroundColor"`
	CustomCSS             string `json:"customCSS"`
}

type Layout struct {
	X        int     `json:"x"`
	Y        int     `json:"y"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Radius   int     `json:"radius"`
	Opacity  float64 `json:"opacity"`
	Rotation float64 `json:"rotation"`
	ZIndex   int     `json:"zIndex"`
}

type HLSSource struct {
	URL string `json:"url"`
}

type ImageSource struct {
	AssetID string `json:"assetId"`
}

type TextSource struct {
	Content           string            `json:"content"`
	FontAssetID       string            `json:"fontAssetId"`
	FontSize          int               `json:"fontSize"`
	Color             string            `json:"color"`
	BackgroundColor   string            `json:"backgroundColor"`
	BackgroundOpacity *float64          `json:"backgroundOpacity,omitempty"`
	BorderColor       string            `json:"borderColor"`
	BorderWidth       int               `json:"borderWidth"`
	LineSpacing       int               `json:"lineSpacing"`
	Remote            *RemoteTextSource `json:"remote,omitempty"`
}

type RemoteTextSource struct {
	URL                    string `json:"url"`
	RefreshIntervalSeconds int    `json:"refreshIntervalSeconds"`
}

type Source struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Kind     SourceKind   `json:"kind"`
	Enabled  bool         `json:"enabled"`
	Layout   Layout       `json:"layout"`
	StyleCSS string       `json:"styleCSS"`
	HLS      *HLSSource   `json:"hls,omitempty"`
	Image    *ImageSource `json:"image,omitempty"`
	Text     *TextSource  `json:"text,omitempty"`
}

type Asset struct {
	ID        string    `json:"id"`
	Kind      AssetKind `json:"kind"`
	Name      string    `json:"name"`
	FileName  string    `json:"fileName"`
	Path      string    `json:"path"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
}

type HLSOutput struct {
	SegmentDuration int    `json:"segmentDuration"`
	ListSize        int    `json:"listSize"`
	Path            string `json:"path"`
	PublicPath      string `json:"publicPath"`
}

type YouTubeOutput struct {
	RTMPURL        string   `json:"rtmpUrl"`
	StreamKey      string   `json:"streamKey"`
	Preset         string   `json:"preset"`
	AdditionalArgs []string `json:"additionalArgs"`
}

type OutputSettings struct {
	Mode           OutputMode    `json:"mode"`
	FrameRate      int           `json:"frameRate"`
	VideoBitrate   string        `json:"videoBitrate"`
	AudioBitrate   string        `json:"audioBitrate"`
	AudioSourceID  string        `json:"audioSourceId"`
	AdditionalArgs []string      `json:"additionalArgs"`
	HLS            HLSOutput     `json:"hls"`
	YouTube        YouTubeOutput `json:"youTube"`
}

type ProjectState struct {
	Canvas  Canvas         `json:"canvas"`
	Sources []Source       `json:"sources"`
	Assets  []Asset        `json:"assets"`
	Output  OutputSettings `json:"output"`
}

type StreamStatus struct {
	Running   bool       `json:"running"`
	Mode      OutputMode `json:"mode"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	PID       int        `json:"pid,omitempty"`
	Command   []string   `json:"command"`
	LastError string     `json:"lastError,omitempty"`
}

type StateResponse struct {
	Project ProjectState `json:"project"`
	Stream  StreamStatus `json:"stream"`
}
