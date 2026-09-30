package ui

import "testing"

// The sentinels under -s. "index:" with nothing after it was the answer for a
// staged deletion, which could then never look changed — and for every path in
// the changeset when the index could not be read at all, which switched change
// detection off for the session with nothing to say so.
func TestIndexKey_DistinguishesItsFailures(t *testing.T) {
	t.Parallel()
	staged := map[string]string{"kept.ts": "abc123"}

	present := indexKey(staged, "kept.ts", true)
	deleted := indexKey(staged, "gone.ts", true)
	unreadable := indexKey(nil, "kept.ts", false)

	for _, pair := range [][2]string{
		{present, deleted},
		{present, unreadable},
		{deleted, unreadable},
	} {
		if pair[0] == pair[1] {
			t.Errorf("two different situations key to the same thing: %q", pair[0])
		}
	}
	if present != "index:abc123" {
		t.Errorf("a staged file keys to %q", present)
	}
}

// And an unreadable index must not make every path look identical, which is
// what makes the whole session stop noticing changes.
func TestIndexKey_AnUnreadableIndexDoesNotFlattenEveryPath(t *testing.T) {
	t.Parallel()
	// It is the same sentinel per path, which is correct — nothing is known —
	// but the point is that it is distinguishable from a real key, so the
	// first successful read after it moves every fingerprint.
	if indexKey(nil, "a.ts", false) == indexKey(map[string]string{"a.ts": ""}, "a.ts", true) {
		t.Error("an unreadable index is indistinguishable from an empty staged oid")
	}
}
