package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

var (
	lexerCache   sync.Map // ext -> chroma.Lexer
	chromaStyle  *chroma.Style
	chromaStyleMu sync.Once
)

// initChromaStyle initializes the chroma style (call once).
func initChromaStyle(styleName string) {
	chromaStyleMu.Do(func() {
		chromaStyle = styles.Get(styleName)
		if chromaStyle == nil {
			chromaStyle = styles.Get("monokai")
		}
	})
}

// getLexer returns a cached Chroma lexer for the given filename.
func getLexer(filename string) chroma.Lexer {
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = filepath.Base(filename)
	}

	if cached, ok := lexerCache.Load(ext); ok {
		return cached.(chroma.Lexer)
	}

	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)
	lexerCache.Store(ext, lexer)
	return lexer
}

// highlightLine applies syntax highlighting to a code line.
// It applies Chroma foreground colors but preserves the background from bgColor.
func highlightLine(content, filename, bgColor string) string {
	if chromaStyle == nil || content == "" {
		return content
	}

	lexer := getLexer(filename)
	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return content
	}

	var b strings.Builder
	for _, token := range iterator.Tokens() {
		entry := chromaStyle.Get(token.Type)
		fg := tokenForeground(entry)
		if fg != "" {
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(fg))
			if bgColor != "" {
				style = style.Background(lipgloss.Color(bgColor))
			}
			b.WriteString(style.Render(token.Value))
		} else if bgColor != "" {
			b.WriteString(lipgloss.NewStyle().Background(lipgloss.Color(bgColor)).Render(token.Value))
		} else {
			b.WriteString(token.Value)
		}
	}
	return b.String()
}

// tokenForeground extracts the hex foreground color from a chroma style entry.
func tokenForeground(entry chroma.StyleEntry) string {
	if entry.Colour.IsSet() {
		return fmt.Sprintf("#%06x", entry.Colour&0xFFFFFF)
	}
	return ""
}
