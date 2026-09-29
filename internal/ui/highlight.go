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
	lexerCache sync.Map // ext -> chroma.Lexer
	styleCache sync.Map // chroma style name -> *chroma.Style
)

// chromaStyleFor resolves a theme's Chroma style, once per name.
//
// This used to be a package-level *chroma.Style behind a sync.Once, then
// behind an RWMutex, and both were wrong in the same way: one global palette
// while the styles are per renderer. A renderer built in a tea.Cmd goroutine
// resolved the style and then rendered, non-atomically, so a theme switch
// between the two produced a diff painted with one theme's backgrounds and
// another's syntax colours. It also made two parallel tests fight over the
// same variable.
//
// Nil means no highlighting, which is what --no-color asks for.
func chromaStyleFor(name string) *chroma.Style {
	if name == theme.NoHighlight {
		return nil
	}
	if cached, ok := styleCache.Load(name); ok {
		return cached.(*chroma.Style)
	}
	// styles.Get never returns nil — it hands back Chroma's own Fallback for a
	// name it does not know, which paints almost nothing. A registry lookup is
	// what makes the monokai fallback real.
	if _, ok := styles.Registry[name]; !ok {
		name = "monokai"
	}
	style := styles.Get(name)
	styleCache.Store(name, style)
	return style
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
func highlightLine(style *chroma.Style, content, filename, bgColor string) string {
	return highlightSpan(style, content, filename, bgColor, lipgloss.Style{}, span{})
}

// highlightSpan is highlightLine with one range of runes painted differently —
// the part of a split-view line that differs from the line it is paired with.
//
// The whole line is lexed once and the *token values* are split at the span's
// boundaries, never the line before lexing: splitting first would change how
// the text tokenises, so a span that happened to start mid-string would
// recolour the rest of the line.
func highlightSpan(style *chroma.Style, content, filename, bgColor string, emph lipgloss.Style, s span) string {
	if content == "" {
		return content
	}
	if style == nil {
		// Highlighting is off, which is also the only situation where the
		// emphasis has no background to use. There is nothing to paint with,
		// so the line goes out as it is — the +/- and the line's own
		// background still say what changed.
		return content
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
		// The span was measured against the line as it came in, so the offset
		// has to advance by what the token was, not by what is left of it
		// after the strip. Chroma normalises a bare CR to a newline, so a CR
		// anywhere before the span used to shift every later boundary by one
		// and the emphasis landed on the wrong rune.
		width := len([]rune(token.Value))
		token.Value = strings.ReplaceAll(token.Value, "\n", "")
		if token.Value != "" {
			fg := tokenForeground(style.Get(token.Type))
			for _, part := range s.split(token.Value, offset) {
				b.WriteString(paint(part.text, fg, bgColor, emph, part.emph))
			}
		}
		offset += width
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
	// No short-circuit on an "empty" style: Style.String() is Render(""), which
	// is blind to text attributes, so an emphasis carrying only an underline
	// looked empty and lost it. Rendering an actually-empty style returns the
	// text unchanged anyway.
	return style.Render(text)
}

// tokenForeground extracts the hex foreground color from a chroma style entry.
func tokenForeground(entry chroma.StyleEntry) string {
	if entry.Colour.IsSet() {
		return entry.Colour.String()
	}
	return ""
}
