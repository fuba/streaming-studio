package model

func DefaultProjectState() ProjectState {
	return ProjectState{
		Canvas: Canvas{
			Width:                 1280,
			Height:                720,
			BackgroundColor:       "#111827",
			EditorBackgroundColor: "#020202",
		},
		Sources: []Source{},
		Assets:  []Asset{},
		Output: OutputSettings{
			Mode:         OutputModeHLS,
			FrameRate:    30,
			VideoBitrate: "4500k",
			AudioBitrate: "160k",
			HLS: HLSOutput{
				SegmentDuration: 2,
				ListSize:        6,
				Path:            "output/hls/live.m3u8",
				PublicPath:      "/live/live.m3u8",
			},
			YouTube: YouTubeOutput{
				RTMPURL:        "rtmp://a.rtmp.youtube.com/live2",
				Preset:         "youtube-default",
				AdditionalArgs: []string{},
			},
			AdditionalArgs: []string{},
		},
	}
}
