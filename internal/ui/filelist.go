package ui

import "strings"

// filePanelPadding is the column FileItem and FileSelected reserve on the left
// (PaddingLeft(1) in styles.go). Row arithmetic has to allow for it, or a row
// comes out a column wider than the panel and lipgloss wraps it.
const filePanelPadding = 1

// The changed-file list.
//
// It is the primary navigator, so it has to say which file each row is and
// roughly what happened to it, in 35 columns. The two things it used to get
// wrong were both about identity: it showed a bare basename, so src/a/index.ts
// and src/b/index.ts read the same, and it stopped drawing at the panel height
// with no way to reach the rest.

// shortNames maps each path to the shortest tail of its directories that tells
// it apart from every other path in the set.
//
// A basename is enough for most files and is what the eye reads first, so the
// directories are added only where they earn their place — one level at a
// time, and only for the paths that actually clash.
func shortNames(paths []string) map[string]string {
	out := make(map[string]string, len(paths))
	segments := make(map[string][]string, len(paths))
	for _, p := range paths {
		segments[p] = strings.Split(p, "/")
	}

	remaining := append([]string(nil), paths...)
	for depth := 1; len(remaining) > 0; depth++ {
		// Group what is left by its last `depth` segments.
		groups := map[string][]string{}
		for _, p := range remaining {
			groups[tail(segments[p], depth)] = append(groups[tail(segments[p], depth)], p)
		}

		var clashing []string
		for name, group := range groups {
			// One path with that tail, or a tail that is already the whole
			// path — nothing longer would tell them apart.
			if len(group) == 1 || depth >= longest(segments, group) {
				for _, p := range group {
					out[p] = name
				}
				continue
			}
			clashing = append(clashing, group...)
		}
		remaining = clashing
	}
	return out
}

// tail is the last n segments, joined.
func tail(segments []string, n int) string {
	if n >= len(segments) {
		return strings.Join(segments, "/")
	}
	return strings.Join(segments[len(segments)-n:], "/")
}

// longest is the deepest path in a group, in segments.
func longest(segments map[string][]string, group []string) int {
	deepest := 0
	for _, p := range group {
		deepest = max(deepest, len(segments[p]))
	}
	return deepest
}
