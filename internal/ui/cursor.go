package ui

// Diff cursor movement and viewport synchronisation.

// moveCursor shifts the cursor by delta lines, clamped to the diff, and keeps
// the viewport window over it.
func (m Model) moveCursor(delta int) Model {
	return m.setCursor(m.diffCursor + delta)
}

// setCursor moves the cursor to an absolute line, clamped to the diff.
func (m Model) setCursor(idx int) Model {
	if m.renderer == nil {
		return m
	}
	last := m.renderer.LineCount() - 1
	if last < 0 {
		m.diffCursor = 0
		return m
	}
	m.diffCursor = min(max(idx, 0), last)
	return m.syncCursorViewport()
}

// syncCursorViewport scrolls the viewport just enough to keep the cursor
// visible, and refreshes the rendered content so the marker follows.
func (m Model) syncCursorViewport() Model {
	if m.renderer == nil {
		return m
	}
	// Scroll in display rows: in split view several source lines share a row.
	row, ok := m.renderer.RowFor(m.diffCursor)
	h := m.viewport.Height
	if ok && h > 0 {
		switch {
		case row < m.viewport.YOffset:
			m.viewport.SetYOffset(row)
		case row >= m.viewport.YOffset+h:
			m.viewport.SetYOffset(row - h + 1)
		}
	}
	content := m.renderer.Content(m.diffCursor)
	m.lastDiffContent = content
	m.viewport.SetContent(content)
	return m
}

// nextHunk and prevHunk move the cursor between hunks, staying put when there
// is no hunk in that direction.
func (m Model) nextHunk() Model {
	if m.renderer == nil {
		return m
	}
	if idx, ok := m.renderer.Parsed().NextHunkLine(m.diffCursor); ok {
		return m.setCursor(idx)
	}
	return m
}

func (m Model) prevHunk() Model {
	if m.renderer == nil {
		return m
	}
	if idx, ok := m.renderer.Parsed().PrevHunkLine(m.diffCursor); ok {
		return m.setCursor(idx)
	}
	return m
}

// cursorAddress resolves the cursor to a reviewable position in the diff.
func (m Model) cursorAddress() (LineAddress, bool) {
	if m.renderer == nil {
		return LineAddress{}, false
	}
	return m.renderer.Parsed().AddressOf(m.diffCursor)
}
