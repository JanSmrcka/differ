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

	// The first probe always reports a change: nothing has been seen yet. The
	// fingerprint is recorded when the refresh it triggers lands, not when it
	// is asked for.
	updated, cmd := m.Update(m.probeCmd()())
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("the first probe scheduled no refresh")
	}
	for _, msg := range fanOut(cmd) {
		updated, _ = m.Update(msg)
		m = updated.(Model)
	}
	if m.repoFingerprint == "" {
		t.Fatal("the refresh landed but no fingerprint was recorded")
	}

	// The second, with nothing touched and the rate limit open, must ask for
	// no work at all — and it has to be the fingerprint that stops it, not the
	// rate limit.
	m.ticksSinceRefresh = refreshEvery
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

// The fingerprint says "the screen matches the repository", so it may only be
// stored once a refresh has actually landed.
//
// Storing it on dispatch made the probe — which the comment calls an
// optimisation, never a gate — into a permanent gate the moment a refresh
// failed: the screen kept the state it had, the fingerprint claimed otherwise,
// and every later probe matched. Before the probe existed the unconditional
// tick healed this within two seconds.
func TestProbe_AFailedRefreshDoesNotFreezeTheScreenForever(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	m := liveModel(t, tr)
	m.ticksSinceRefresh = refreshEvery

	tr.Modify("a.txt", "changed\n")
	updated, cmd := m.Update(m.probeCmd()())
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("the change did not schedule a refresh")
	}

	// The refresh comes back as a failure: git was unreadable for a moment.
	updated, _ = m.Update(filesRefreshedMsg{err: errProbe})
	m = updated.(Model)

	// The repository is still in the state the probe reported, and the screen
	// still does not reflect it — so the next probe must try again.
	m.ticksSinceRefresh = refreshEvery
	_, cmd = m.Update(m.probeCmd()())
	if cmd == nil {
		t.Error("after one failed refresh the screen would never update again")
	}
}

// Two refreshes can be in flight at once — a probe's and one from staging a
// file — and they can land out of order. The older one must not win: it would
// leave the screen showing state the repository has moved past, with a
// fingerprint saying it is current, so no probe would ever correct it.
func TestProbe_AnOlderRefreshDoesNotOverwriteANewerOne(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	tr.CommitFile("b.txt", "one\n", "second")
	m := liveModel(t, tr)

	tr.Modify("a.txt", "changed\n")
	older := m.refreshFilesAs("older", 1)().(filesRefreshedMsg)
	tr.Modify("b.txt", "changed too\n")
	newer := m.refreshFilesAs("newer", 2)().(filesRefreshedMsg)

	if len(older.files) == len(newer.files) {
		t.Fatalf("the two refreshes are indistinguishable (%d files each)", len(older.files))
	}

	// The newer one lands first, then the older one arrives late.
	updated, _ := m.Update(newer)
	m = updated.(Model)
	updated, _ = m.Update(older)
	m = updated.(Model)

	if len(m.files) != len(newer.files) {
		t.Errorf("the late older refresh won: screen shows %d files, repository has %d",
			len(m.files), len(newer.files))
	}
	if m.repoFingerprint != "newer" {
		t.Errorf("the fingerprint is %q — the older refresh's claim won", m.repoFingerprint)
	}
}

// The rate limiter has to advance on its own, driven by real ticks.
//
// Every other rate-limit test here sets m.ticksSinceRefresh by hand, so the
// one line that increments it was exercised by nothing: deleting it left the
// counter at zero for the rest of the session, every change held back forever,
// and the suite green. That is the whole UI frozen.
func TestProbe_TicksAloneAreEnoughToKeepTheScreenUpToDate(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	m := liveModel(t, tr)

	refreshes := 0
	for i := 0; i < 6; i++ {
		tr.Modify("a.txt", strings.Repeat("x", i+1)+"\n")

		// Exactly what the runtime does: a tick, then whatever it asks for.
		updated, cmd := m.Update(tickMsg(time.Now()))
		m = updated.(Model)
		for _, msg := range fanOut(cmd) {
			if _, isTick := msg.(tickMsg); isTick {
				continue // the next tick, not this one's work
			}
			updated, follow := m.Update(msg)
			m = updated.(Model)
			if _, probed := msg.(repoProbedMsg); probed && follow != nil {
				refreshes++
				for _, out := range fanOut(follow) {
					updated, _ = m.Update(out)
					m = updated.(Model)
				}
			}
		}
	}

	// One refresh proves nothing: the model starts with the rate limit open,
	// so the first tick refreshes even if the counter never advances again.
	// Six ticks with a change before each should give about three.
	if refreshes < 2 {
		t.Errorf("six ticks with a change before each produced %d refreshes; "+
			"the rate limit never reopens, so the screen is frozen", refreshes)
	}
}

// The gate itself, isolated from the rate limit.
//
// TestProbe_AnUnchangedRepoCostsNothingBeyondTheProbe could pass with the
// fingerprint comparison deleted, because ticksSinceRefresh happened to be 0
// at the moment it asserted — so the rate limit was returning nil, not the
// gate. Reverting to an unconditional rebuild has to fail something.
func TestProbe_TheGateIsTheFingerprintNotTheRateLimit(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	m := liveModel(t, tr)

	// Settle the fingerprint by letting one refresh land.
	m.ticksSinceRefresh = refreshEvery
	updated, cmd := m.Update(m.probeCmd()())
	m = updated.(Model)
	for _, msg := range fanOut(cmd) {
		updated, _ = m.Update(msg)
		m = updated.(Model)
	}

	// Now hold the rate limit wide open across several probes. Nothing has
	// changed, so nothing may be scheduled — and the only thing that can stop
	// it is the fingerprint.
	for i := 0; i < 3; i++ {
		m.ticksSinceRefresh = refreshEvery * 4
		var follow tea.Cmd
		updated, follow = m.Update(m.probeCmd()())
		m = updated.(Model)
		if follow != nil {
			t.Fatalf("probe %d rebuilt an unchanged repository with the rate limit open", i+1)
		}
	}
}

// Review mode is the case #45 exists for, and nothing checked that it polls at
// all: adding it to the tick's skip list left the suite green.
func TestProbe_EveryModeThatShouldPollDoes(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")

	for _, tc := range []struct {
		name string
		mode viewMode
		poll bool
	}{
		{"file list", modeFileList, true},
		{"diff", modeDiff, true},
		{"review", modeReview, true},
		// Typing a commit message or picking a branch: the screen must not
		// move underneath the input.
		{"commit", modeCommit, false},
		{"branch picker", modeBranchPicker, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := liveModel(t, tr)
			m.mode = tc.mode

			updated, cmd := m.Update(tickMsg(time.Now()))
			m = updated.(Model)
			probed, ticked := collectMsgs(cmd)

			if probed != tc.poll {
				t.Errorf("%v: probed=%v, want %v", tc.name, probed, tc.poll)
			}
			if !ticked {
				t.Errorf("%v: the tick did not schedule the next one", tc.name)
			}
		})
	}
}

// A probe that fails still has to be rate-limited. A repository where it fails
// reliably — a corrupt index, a long rebase — would otherwise rebuild on every
// tick: nine git processes a second, in exactly the degraded state where that
// is least welcome.
func TestProbe_AFailingProbeIsRateLimitedLikeAnyOther(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	m := liveModel(t, tr)

	refreshes := 0
	for i := 0; i < 6; i++ {
		m.ticksSinceRefresh++
		updated, cmd := m.Update(repoProbedMsg{err: errProbe})
		m = updated.(Model)
		if cmd != nil {
			refreshes++
		}
	}
	if refreshes == 0 {
		t.Fatal("a failing probe never refreshed; the screen would be frozen")
	}
	if refreshes > 6/refreshEvery {
		t.Errorf("%d refreshes in 6 failing probes, want at most %d", refreshes, 6/refreshEvery)
	}
}

// A probe that never answers must not wedge the poll loop.
//
// git status can block rather than fail — a hung network mount, a wedged
// fsmonitor — and the in-flight guard is cleared only by the answer. Without a
// release the UI would stop updating with nothing on screen to say so.
func TestProbe_AProbeThatNeverAnswersIsEventuallyReplaced(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	m := liveModel(t, tr)

	// The first tick sends one, and it never comes back.
	updated, cmd := m.Update(tickMsg(time.Now()))
	m = updated.(Model)
	if probed, _ := collectMsgs(cmd); !probed {
		t.Fatal("the first tick did not probe")
	}

	sent := 0
	for i := 0; i < staleProbeTicks*2; i++ {
		updated, cmd = m.Update(tickMsg(time.Now()))
		m = updated.(Model)
		if probed, _ := collectMsgs(cmd); probed {
			sent++
		}
	}
	if sent == 0 {
		t.Errorf("after %d ticks with no answer, no new probe was sent — "+
			"the poll loop is wedged for the rest of the session", staleProbeTicks*2)
	}
}
