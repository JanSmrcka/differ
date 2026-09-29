package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/testutil"
)

// The point of the whole change: an idle session must stop doing work. The tick
// used to rebuild the file list and re-ask about the upstream every two seconds
// whatever had happened — eight git processes a tick, forever.
func TestProbe_AnUnchangedRepoCostsNothingBeyondTheProbe(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	tr.Modify("a.txt", "two\n")
	m := liveModel(t, tr)

	// The first probe always reports a change: nothing has been seen yet.
	cmd := m.probeCmd()
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if m.repoFingerprint == "" {
		t.Fatal("the first probe recorded no fingerprint")
	}

	// The second, with nothing touched, must ask for no work at all.
	_, follow := m.Update(m.probeCmd()())
	if follow != nil {
		t.Error("an unchanged repo still scheduled a refresh")
	}
}

// And it must still notice when something does move, or the screen freezes.
func TestProbe_AChangedRepoSchedulesARefresh(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	m := liveModel(t, tr)

	updated, _ := m.Update(m.probeCmd()())
	m = updated.(Model)

	tr.Modify("a.txt", "changed\n")

	if _, follow := m.Update(m.probeCmd()()); follow == nil {
		t.Fatal("a modified file did not schedule a refresh")
	}
}

// A probe that fails must fall back to refreshing rather than freezing the
// screen: a repo can be mid-rebase, or the git binary can vanish under us.
func TestProbe_AFailedProbeStillRefreshes(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	m := liveModel(t, tr)

	_, follow := m.Update(repoProbedMsg{err: errProbe})
	if follow == nil {
		t.Error("a failed probe stopped the refresh entirely")
	}
}

var errProbe = tea.ErrProgramKilled
