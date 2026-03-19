package stream

import (
	"strings"
	"testing"

	"streaming-studio/internal/model"
)

func TestWrapTextForSourceWrapsLongTextWithinSourceWidth(t *testing.T) {
	t.Parallel()

	source := model.Source{
		Kind: model.SourceKindText,
		Layout: model.Layout{
			Width: 180,
		},
		Text: &model.TextSource{
			FontSize: 40,
		},
	}

	got := wrapTextForSource(source, "ABCDEFGHIJ")
	if got == "ABCDEFGHIJ" {
		t.Fatalf("wrapTextForSource() = %q, want inserted line break", got)
	}
	if !strings.Contains(got, "\n") {
		t.Fatalf("wrapTextForSource() = %q, want newline", got)
	}
}

func TestWrapTextForSourcePreservesExplicitLineBreaks(t *testing.T) {
	t.Parallel()

	source := model.Source{
		Kind: model.SourceKindText,
		Layout: model.Layout{
			Width: 200,
		},
		Text: &model.TextSource{
			FontSize: 32,
		},
	}

	got := wrapTextForSource(source, "1行目\n2行目")
	if got != "1行目\n2行目" {
		t.Fatalf("wrapTextForSource() = %q, want %q", got, "1行目\n2行目")
	}
}
