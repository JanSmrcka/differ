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
	m.diffCursor = clampCursor(idx, m.renderer.LineCount())
	m.cursorPlaced = true
	return m.applyContent(true)
}

// clampCursor keeps an index inside a diff of count lines. An empty diff — a
// binary file, or a new file with no content — addresses line 0.
func clampCursor(idx, count int) int {
	if count <= 0 {
		return 0
	}
	return min(max(idx, 0), count-1)
}

// applyContent re-renders the diff into the viewport.
//
// The viewport is only touched when the rendered content actually changed, so
// the two-second poll cannot undo scrolling the user did themselves with
// pgup/pgdn or the mouse wheel. It scrolls to the cursor when the user moved
// it, or when the content changed underneath and the cursor would otherwise be
// off screen.
func (m Model) applyContent(follow bool) Model {
	if m.renderer == nil {
		return m
	}
	content := m.renderer.Content(m.diffCursor)
	changed := content != m.lastDiffContent
	if changed {
		m.lastDiffContent = content
		m.viewport.SetContent(content)
	}
	if follow || changed {
		m = m.scrollToCursor()
	}
	return m
}

// scrollToCursor scrolls just enough to bring the cursor's row into view.
func (m Model) scrollToCursor() Model {
	if m.renderer == nil {
		return m
	}
	// Scroll in display rows: in split view several source lines share a row,
	// and inline comments add rows of their own.
	row, ok := m.renderer.RowFor(m.diffCursor)
	h := m.viewport.Height
	if !ok || h <= 0 {
		return m
	}
	switch {
	case row < m.viewport.YOffset:
		m.viewport.SetYOffset(row)
	case row >= m.viewport.YOffset+h:
		m.viewport.SetYOffset(row - h + 1)
	}
	return m
}

// fitViewport resizes the viewport when the space available to it has changed,
// and does nothing otherwise so it is cheap to call after every key.
func (m Model) fitViewport() Model {
	if !m.ready || m.viewport.Height == m.listHeight() {
		return m
	}
	return m.resizeViewport()
}

// resizeViewport matches the viewport to the space the panels currently have,
// which changes when the footer grows or shrinks, then keeps the cursor in
// view.
func (m Model) resizeViewport() Model {
	if !m.ready {
		return m
	}
	m.viewport.Width = m.diffWidth()
	// listHeight, not contentHeight: the panel spends two rows on its label
	// and the blank line under it. Using the larger figure clipped the bottom
	// two diff rows while scrollToCursor still counted them as visible.
	m.viewport.Height = m.listHeight()
	return m.applyContent(true)
}

// syncCursorViewport re-renders and follows the cursor, for callers that just
// changed what the diff should look like.
func (m Model) syncCursorViewport() Model { return m.applyContent(true) }

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
