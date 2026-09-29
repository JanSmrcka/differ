package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/testutil"
)

// The point of the whole change: an idle session must stop doing work. The tick
// used to rebuild the file list and re-ask about the upstream every two seconds
// whatever had happened — eight git processes a tick, forever.
//
// allowRefresh opens the rate limit, which the tick normally fills.
func TestProbe_AnUnchangedRepoCostsNothingBeyondTheProbe(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	tr.Modify("a.txt", "two\n")
	m := liveModel(t, tr)
	m.ticksSinceRefresh = refreshEvery

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
	m.ticksSinceRefresh = refreshEvery

	updated, _ := m.Update(m.probeCmd()())
	m = updated.(Model)
	m.ticksSinceRefresh = refreshEvery

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

// The wiring, end to end through handleTick. The probe tests drove probeCmd
// directly, so three separate sabotages went unnoticed: handleTick never
// probing at all, and refreshEverythingCmd dropping either half of its work.
// The first of those would have stopped the poll loop refreshing anything.
func TestProbe_TheTickProbesAndThenRefreshes(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	m := liveModel(t, tr)

	// A tick has to produce a probe, not just another tick.
	updated, cmd := m.Update(tickMsg(time.Now()))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("the tick produced no command")
	}
	probed, ticked := collectMsgs(cmd)
	if !probed {
		t.Error("the tick did not probe the repository")
	}
	if !ticked {
		t.Error("the tick did not schedule the next one")
	}

	// And the refresh it schedules has to cover both the file list and the
	// header, or one of them silently stops updating.
	tr.Modify("a.txt", "two\n")
	kinds := map[string]bool{}
	for _, msg := range fanOut(m.refreshEverythingCmd()) {
		switch msg.(type) {
		case filesRefreshedMsg:
			kinds["files"] = true
		case upstreamStatusMsg:
			kinds["upstream"] = true
		}
	}
	if !kinds["files"] {
		t.Error("the refresh does not rebuild the file list")
	}
	if !kinds["upstream"] {
		t.Error("the refresh does not update the upstream, so the header goes stale")
	}
}

// A probe that is still out must not be joined by another.
func TestProbe_DoesNotPileUpWhenOneIsStillOut(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	m := liveModel(t, tr)

	updated, _ := m.Update(tickMsg(time.Now()))
	m = updated.(Model)
	if !m.probing {
		t.Fatal("the first tick did not mark a probe as out")
	}

	updated, cmd := m.Update(tickMsg(time.Now()))
	m = updated.(Model)
	if probed, _ := collectMsgs(cmd); probed {
		t.Error("a second probe was started while the first was still out")
	}

	// The answer arriving clears the way for the next one.
	updated, _ = m.Update(repoProbedMsg{fingerprint: "x"})
	m = updated.(Model)
	if m.probing {
		t.Error("the probe's answer did not clear the in-flight flag")
	}
}

// fanOut runs a command and everything a tea.Batch fans out to, returning the
// messages produced.
func fanOut(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, fanOut(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func collectMsgs(cmd tea.Cmd) (probed, ticked bool) {
	for _, msg := range fanOut(cmd) {
		switch msg.(type) {
		case repoProbedMsg:
			probed = true
		case tickMsg:
			ticked = true
		}
	}
	return probed, ticked
}

// The coalescing the issue asks for: "a coding agent rewrites many files in
// quick succession and should cause one refresh, not twenty".
//
// The probe runs every tick so a change is seen within a second, but acting on
// it is rate-limited. Without that, a sustained burst moves the fingerprint on
// every probe and costs nine git processes a second — more churn than the
// two-second rebuild this replaced, in exactly the scenario the issue is about.
func TestProbe_ASustainedBurstDoesNotRefreshEveryTick(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	m := liveModel(t, tr)

	refreshes := 0
	for i := 0; i < 8; i++ {
		// Something changes before every single probe.
		tr.Modify("a.txt", strings.Repeat("x", i+1)+"\n")

		m.ticksSinceRefresh++ // what handleTick does
		updated, cmd := m.Update(m.probeCmd()())
		m = updated.(Model)
		if cmd != nil {
			refreshes++
		}
	}

	if refreshes == 8 {
		t.Error("every probe triggered a refresh; the burst was not coalesced at all")
	}
	if refreshes == 0 {
		t.Fatal("a continuous burst never refreshed; the screen would be frozen")
	}
	if refreshes > 8/refreshEvery {
		t.Errorf("%d refreshes in 8 ticks, want at most %d", refreshes, 8/refreshEvery)
	}
}

// A change seen but not yet acted on must not be dropped. The fingerprint is
// only stored when the refresh actually happens, so the next probe still finds
// a difference — otherwise a write landing inside the rate limit would be
// silently forgotten.
func TestProbe_AChangeHeldBackByTheRateLimitIsNotLost(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	m := liveModel(t, tr)

	// Settle: one refresh, then the limit is closed again.
	m.ticksSinceRefresh = refreshEvery
	updated, _ := m.Update(m.probeCmd()())
	m = updated.(Model)

	// A change arrives while the limit is closed.
	m.ticksSinceRefresh = 0
	tr.Modify("a.txt", "changed\n")
	updated, cmd := m.Update(m.probeCmd()())
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("the rate limit did not hold the refresh back")
	}

	// Nothing else happens, but the limit opens: the held change must surface.
	m.ticksSinceRefresh = refreshEvery
	_, cmd = m.Update(m.probeCmd()())
	if cmd == nil {
		t.Error("a change held back by the rate limit was dropped, not delayed")
	}
}
