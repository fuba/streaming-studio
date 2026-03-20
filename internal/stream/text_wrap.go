package stream

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"streaming-studio/internal/model"
)

const (
	defaultWrapFontSize = 42
)

func wrapTextForSource(source model.Source, content string) string {
	if source.Text == nil {
		return content
	}

	width := source.Layout.Width
	if width <= 0 {
		return content
	}

	fontSize := source.Text.FontSize
	if fontSize <= 0 {
		fontSize = defaultWrapFontSize
	}

	lines := strings.Split(content, "\n")
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, wrapTextLine(line, float64(width), float64(fontSize))...)
	}
	return strings.Join(wrapped, "\n")
}

func wrapTextLine(line string, maxWidth, fontSize float64) []string {
	if strings.TrimSpace(line) == "" {
		return []string{line}
	}

	runes := []rune(line)
	lines := make([]string, 0, 1)
	var builder strings.Builder
	currentWidth := 0.0

	for _, currentRune := range runes {
		runeWidth := estimateRuneWidth(currentRune, fontSize)
		if builder.Len() > 0 && currentWidth+runeWidth > maxWidth {
			lines = append(lines, builder.String())
			builder.Reset()
			currentWidth = 0
		}
		builder.WriteRune(currentRune)
		currentWidth += runeWidth
	}

	if builder.Len() > 0 || len(lines) == 0 {
		lines = append(lines, builder.String())
	}
	return lines
}

func estimateRuneWidth(value rune, fontSize float64) float64 {
	switch {
	case value == '\t':
		return fontSize * 1.4
	case unicode.IsSpace(value):
		return fontSize * 0.35
	case isWideRune(value):
		return fontSize
	case value < utf8.RuneSelf:
		if unicode.IsUpper(value) {
			return fontSize * 0.68
		}
		if unicode.IsPunct(value) {
			return fontSize * 0.45
		}
		return fontSize * 0.58
	default:
		return fontSize * 0.8
	}
}

func isWideRune(value rune) bool {
	return unicode.In(value,
		unicode.Han,
		unicode.Hiragana,
		unicode.Katakana,
		unicode.Hangul,
		unicode.Bopomofo,
		unicode.Cyrillic,
		unicode.Greek,
	)
}
