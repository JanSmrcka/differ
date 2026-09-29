package ui

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/theme"
)

var (
	lexerCache    sync.Map // ext -> chroma.Lexer
	chromaStyle   *chroma.Style
	chromaStyleMu sync.Once
)

// initChromaStyle initializes the chroma style (call once).
func initChromaStyle(styleName string) {
	chromaStyleMu.Do(func() {
		// theme.NoHighlight means the user asked for no colour, so leave the
		// style nil and highlightLine returns the text untouched. An empty
		// name is different: that is "unset", and falls back to a default.
		if styleName == theme.NoHighlight {
			return
		}
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
	return highlightSpan(content, filename, bgColor, lipgloss.Style{}, span{})
}

// highlightSpan is highlightLine with one range of runes painted differently —
// the part of a split-view line that differs from the line it is paired with.
//
// The whole line is lexed once and the *token values* are split at the span's
// boundaries, never the line before lexing: splitting first would change how
// the text tokenises, so a span that happened to start mid-string would
// recolour the rest of the line.
func highlightSpan(content, filename, bgColor string, emph lipgloss.Style, s span) string {
	if content == "" {
		return content
	}
	if chromaStyle == nil {
		// No syntax highlighting — but the emphasis still has to show, and
		// without colour it is the underline doing the work.
		if !s.marks() {
			return content
		}
		var b strings.Builder
		for _, part := range s.split(content, 0) {
			if part.emph {
				b.WriteString(emph.Render(part.text))
				continue
			}
			b.WriteString(part.text)
		}
		return b.String()
	}

	lexer := getLexer(filename)
	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return content
	}

	var b strings.Builder
	offset := 0
	for _, token := range iterator.Tokens() {
		// Chroma appends a newline to its input and coalesces it into the last
		// token, so any token running to end of line carries one — the tail of
		// an open block comment, an unterminated string, a CRLF line ending, a
		// non-breaking space. Written out verbatim it makes one row of output
		// into two, and the renderer addresses rows by index: DisplayRows then
		// disagrees with Content, and RowFor points at the wrong row for
		// everything below.
		token.Value = strings.ReplaceAll(token.Value, "\n", "")
		if token.Value == "" {
			continue
		}
		fg := tokenForeground(chromaStyle.Get(token.Type))
		for _, part := range s.split(token.Value, offset) {
			b.WriteString(paint(part.text, fg, bgColor, emph, part.emph))
		}
		offset += len([]rune(token.Value))
	}
	return b.String()
}

// paint renders one piece of a token: the token's own foreground, on the
// line's background, or on the emphasis style when the piece is inside the
// changed span.
func paint(text, fg, bgColor string, emph lipgloss.Style, emphasised bool) string {
	style := lipgloss.NewStyle()
	switch {
	case emphasised:
		style = emph
	case bgColor != "":
		style = style.Background(lipgloss.Color(bgColor))
	}
	if fg != "" {
		style = style.Foreground(lipgloss.Color(fg))
	}
	if style.String() == "" && fg == "" {
		return text
	}
	return style.Render(text)
}

// tokenForeground extracts the hex foreground color from a chroma style entry.
func tokenForeground(entry chroma.StyleEntry) string {
	if entry.Colour.IsSet() {
		return entry.Colour.String()
	}
	return ""
}
