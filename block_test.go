package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// a config with the given blocks plus one cycler item, at a pinned clock
func withBlocks(t *testing.T, blocks ...block) model {
	c := &config{}
	c.Items = append(c.Items, item{
		ID: newID(), Name: "Task", Seconds: 60, ActiveDays: allDays(),
		Source: srcStatic, SourceConfig: "task text",
	})
	for i := range blocks {
		b := blocks[i]
		if b.ID == "" {
			b.ID = newID()
		}
		if b.ActiveDays == nil {
			b.ActiveDays = allDays()
		}
		if b.Type == "" {
			b.Type = blkStandard
		}
		if b.Color == "" {
			b.Color = defaultBlockColor
		}
		if b.Source == "" {
			b.Source = srcStatic
		}
		c.Blocks = append(c.Blocks, b)
	}
	c.sortBlocks()
	c.save()
	return newModel()
}

func at(h, min int) time.Time {
	return time.Date(2026, 10, 21, h, min, 0, 0, time.Local)
}

func hm(h, min int) int { return h*60 + min }

func TestLegacyConfigHasNoBlocks(t *testing.T) {
	setup(t)
	writeJSON(configPath(), map[string]any{
		"items": []map[string]any{
			{"id": "a", "name": "A", "seconds": 60, "source": "static", "activeDays": []int{0, 1, 2, 3, 4, 5, 6}},
		},
	})
	m := newModel()
	if len(m.c.Blocks) != 0 || !m.blocksOff() {
		t.Fatalf("a config with no blocks key should load as none: %+v", m.c.Blocks)
	}
	if m.scr != scrCycler {
		t.Fatal("and should behave exactly as before")
	}
	if ph, _, _ := m.phase(); ph != phaseNoBlocks {
		t.Fatalf("want phaseNoBlocks, got %d", ph)
	}
}

func TestLoadConfigRepairsBlocks(t *testing.T) {
	setup(t)
	writeJSON(configPath(), &config{Blocks: []block{
		{ID: "ok", Title: "Good", Type: blkStandard, Color: "blue", Start: hm(9, 0), End: hm(10, 0), ActiveDays: allDays(), Source: srcStatic},
		{ID: "bad", Title: "Inverted", Type: blkStandard, Color: "blue", Start: hm(11, 0), End: hm(10, 0), ActiveDays: allDays(), Source: srcStatic},
		{ID: "junk", Title: "Junk", Type: "nonsense", Color: "chartreuse", Start: hm(12, 0), End: hm(13, 0), ActiveDays: allDays(), Source: "nope"},
	}})
	c := loadConfig()
	if len(c.Blocks) != 2 {
		t.Fatalf("a window that never opens should be dropped: %+v", c.Blocks)
	}
	j := c.blockByID("junk")
	if j == nil || j.Type != blkStandard || j.Color != defaultBlockColor || j.Source != srcStatic {
		t.Fatalf("bad fields should be repaired: %+v", j)
	}
}

func TestPhaseAcrossTheDay(t *testing.T) {
	setClock := setup(t)
	setClock(at(8, 0))
	m := withBlocks(t,
		block{Title: "Morning", Start: hm(9, 0), End: hm(10, 0)},
		block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)},
	)

	cases := []struct {
		h, min int
		want   dayPhase
	}{
		{8, 0, phaseSleep},
		{9, 30, phaseBlock},
		{10, 30, phaseGap},
		{11, 30, phaseBlock},
		{13, 0, phaseDone},
	}
	for _, c := range cases {
		setClock(at(c.h, c.min))
		if ph, _, _ := m.phase(); ph != c.want {
			t.Errorf("%02d:%02d want phase %d, got %d", c.h, c.min, c.want, ph)
		}
	}
}

func TestNoBlocksTodayIsAllDayFreeTime(t *testing.T) {
	setClock := setup(t)
	// 2026-10-21 is a Wednesday; the block only runs on Sunday
	setClock(at(10, 0))
	m := withBlocks(t, block{
		Title: "Sunday only", Start: hm(9, 0), End: hm(10, 0),
		ActiveDays: []int{int(time.Sunday)},
	})
	ph, _, _ := m.phase()
	if ph != phaseNoBlocks {
		t.Fatalf("want phaseNoBlocks, got %d", ph)
	}
	if m.blocksOff() {
		t.Fatal("blocks are configured, so the day is free time rather than the plain cycler")
	}
	if v := m.View(); !strings.Contains(v, "FREE TIME") {
		t.Fatalf("want all-day free time:\n%s", v)
	}
}

func TestExtendCancelsAndCuts(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t,
		block{Title: "A", Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "B", Start: hm(12, 0), End: hm(12, 30)},
		block{Title: "C", Start: hm(12, 30), End: hm(13, 0)},
	)
	m.s.endBlockAt(m.c, 0, hm(12, 50))

	bs := m.s.dayBlocks(m.c)
	if bs[0].end != hm(12, 50) {
		t.Fatalf("A should end at 12:50, got %d", bs[0].end)
	}
	if !bs[1].cancelled {
		t.Fatal("B is wholly inside the extension and should be cancelled")
	}
	if bs[2].cancelled || bs[2].start != hm(12, 50) {
		t.Fatalf("C should survive, starting at 12:50: %+v", bs[2])
	}
}

func TestFinishEarlyOnlyMovesTheNextStart(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t,
		block{Title: "A", Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "B", Start: hm(12, 0), End: hm(12, 30)},
	)
	m.s.endBlockAt(m.c, 0, hm(11, 30))

	bs := m.s.dayBlocks(m.c)
	if bs[1].start != hm(11, 30) || bs[1].end != hm(12, 30) {
		t.Fatalf("B should start early and keep its end: %+v", bs[1])
	}
	if bs[1].cancelled {
		t.Fatal("B should not be cancelled")
	}
}

func TestCancelledBlockStaysCancelled(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t,
		block{Title: "A", Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "B", Start: hm(12, 0), End: hm(12, 30)},
		block{Title: "C", Start: hm(12, 30), End: hm(14, 0)},
	)
	m.s.endBlockAt(m.c, 0, hm(12, 30))
	if !m.s.dayBlocks(m.c)[1].cancelled {
		t.Fatal("B should be cancelled")
	}

	// pulling the boundary back does not resurrect it
	m.s.endBlockAt(m.c, 0, hm(12, 10))
	bs := m.s.dayBlocks(m.c)
	if !bs[1].cancelled {
		t.Fatal("B should stay cancelled")
	}
	// and it never becomes current
	setClock(at(12, 15))
	_, cur, _ := m.phase()
	if cur.Title == "B" {
		t.Fatal("a cancelled block should never run")
	}
}

func TestOverlapValidation(t *testing.T) {
	setup(t)
	c := &config{Blocks: []block{
		{ID: "a", Title: "A", Start: hm(9, 0), End: hm(10, 0), ActiveDays: weekdayDays()},
	}}
	clash := block{ID: "b", Title: "B", Start: hm(9, 30), End: hm(10, 30), ActiveDays: weekdayDays()}
	if o := c.overlaps(clash); o == nil || o.Title != "A" {
		t.Fatal("a same-day overlap should be rejected")
	}

	sunday := clash
	sunday.ActiveDays = []int{int(time.Sunday)}
	if o := c.overlaps(sunday); o != nil {
		t.Fatalf("the same window on another day is fine, got %+v", o)
	}

	abut := block{ID: "c", Title: "C", Start: hm(10, 0), End: hm(11, 0), ActiveDays: weekdayDays()}
	if o := c.overlaps(abut); o != nil {
		t.Fatalf("a block starting exactly where another ends is fine, got %+v", o)
	}
}

func TestCyclerOverrunHoldsBlock(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	var notes []string
	notifyTextFn = func(s string) { notes = append(notes, s) }
	t.Cleanup(func() { notifyTextFn = notifyText })

	m := withBlocks(t,
		block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "After", Start: hm(12, 0), End: hm(13, 0)},
	)
	m = send(m, " ") // start the timer
	if m.s.Timer != timerGoing {
		t.Fatal("timer should be running")
	}

	setClock(at(12, 10))
	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)

	if m.s.Held == "" {
		t.Fatal("a running cycler block should be held past its end")
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "still running") {
		t.Fatalf("want one overrun notification, got %v", notes)
	}
	ph, b, _ := m.phase()
	if ph != phaseBlock || b.Title != "Cycle" {
		t.Fatalf("the held block should still be current, got %d %q", ph, b.Title)
	}

	// finishing the item releases it and pulls the next block up to now
	m = send(m, "enter", "y")
	if m.s.Held != "" {
		t.Fatal("completing the item should release the block")
	}
	bs := m.s.dayBlocks(m.c)
	if bs[0].end != hm(12, 10) || bs[1].start != hm(12, 10) {
		t.Fatalf("the boundary should move to 12:10: %+v", bs)
	}
}

func TestOverrunIgnoresIdleTimer(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t,
		block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "After", Start: hm(12, 0), End: hm(13, 0)},
	)
	if m.s.Timer != timerIdle {
		t.Fatal("timer should be idle")
	}
	setClock(at(12, 10))
	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)

	if m.s.Held != "" {
		t.Fatal("an idle timer is nothing to interrupt")
	}
	if ph, b, _ := m.phase(); ph != phaseBlock || b.Title != "After" {
		t.Fatalf("the day should have moved on, got %d %q", ph, b.Title)
	}
}

func TestOverrideDroppedOnEditAndDelete(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t,
		block{Title: "A", Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "B", Start: hm(12, 0), End: hm(13, 0)},
	)
	id := m.c.Blocks[0].ID
	m.s.endBlockAt(m.c, 0, hm(11, 45))
	if len(m.s.Blocks) == 0 {
		t.Fatal("setup: want an override")
	}

	// editing the window drops the drift measured against the old one
	m = send(m, "C", "tab", "e")
	m = editField(m, fEnd)
	m = send(m, "backspace", "backspace")
	m = typeText(m, "11")
	m = send(m, "tab", "backspace", "backspace")
	m = typeText(m, "45")
	m = saveFormKeys(m)
	if m.scr != scrConfig {
		t.Fatalf("save failed: %+v", m.f)
	}
	if _, ok := m.s.Blocks[id]; ok {
		t.Fatal("a moved window should drop its override")
	}

	// and deleting drops it too
	m.s.endBlockAt(m.c, 0, hm(11, 45))
	m = send(m, "d", "y")
	if _, ok := m.s.Blocks[id]; ok {
		t.Fatal("a deleted block should drop its override")
	}
}

func TestBlockFormInvalidTime(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t, block{Title: "A", Start: hm(11, 0), End: hm(12, 0)})

	m = send(m, "C", "tab", "e")
	m = editField(m, fEnd)
	m = send(m, "backspace", "backspace")
	m = typeText(m, "12")
	m = send(m, "tab", "backspace", "backspace")
	m = typeText(m, "90")
	m = send(m, "esc")

	if m.f.clockValid(fEnd) {
		t.Fatal("12:90 is not a time")
	}
	if m.f.saveReady() {
		t.Fatal("save should be inert while a time cannot be read")
	}
	if !strings.Contains(m.View(), "Invalid!") {
		t.Fatalf("want the invalid marker:\n%s", m.View())
	}
	m = saveFormKeys(m)
	if m.scr != scrForm {
		t.Fatal("save should not have gone through")
	}

	// correcting it lets the save land
	m = editField(m, fEnd)
	m = send(m, "tab", "backspace", "backspace")
	m = typeText(m, "30")
	m = saveFormKeys(m)
	if m.scr != scrConfig || m.c.Blocks[0].End != hm(12, 30) {
		t.Fatalf("want 12:30, got %+v", m.c.Blocks[0])
	}
}

func TestBlockFormNavigation(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t, block{Title: "A", Start: hm(11, 0), End: hm(12, 0)})

	// a new block opens already editing; an edited one opens selected
	m = send(m, "C", "tab", "a")
	if !m.f.editing || m.f.focus != fTitle {
		t.Fatalf("a new block should open editing its title: %+v", m.f.focus)
	}
	m = send(m, "esc", "esc")
	m = send(m, "e")
	if m.f.editing {
		t.Fatal("an existing block should open selected, not editing")
	}

	// tab walks the two cells of a time field, then leaves for the next line
	m = editField(m, fStart)
	if m.f.cell != 0 {
		t.Fatal("editing starts on the hour")
	}
	m = send(m, "tab")
	if m.f.cell != 1 || m.f.focus != fStart {
		t.Fatalf("tab should reach the minute cell: %d %d", m.f.focus, m.f.cell)
	}
	m = send(m, "shift+tab")
	if m.f.cell != 0 {
		t.Fatal("shift+tab should return to the hour")
	}
	m = send(m, "tab", "tab")
	if m.f.focus != fEnd {
		t.Fatalf("tab off the last cell should leave the field, got %d", m.f.focus)
	}

	// switching to cycler disables the source lines, and navigation skips them
	m = toField(m, fType)
	m = send(m, "right")
	if !m.f.isCycler() {
		t.Fatal("want a cycler block")
	}
	seen := map[field]bool{}
	for range 12 {
		m = send(m, "j")
		seen[m.f.focus] = true
	}
	for _, skipped := range []field{fColor, fSrcType, fSrc} {
		if seen[skipped] {
			t.Fatalf("a disabled line should be skipped, landed on %d", skipped)
		}
	}
}

func TestEscCommitsRatherThanReverting(t *testing.T) {
	setup(t)
	m := withItems(t, "A")
	m = send(m, "C", "e")
	m = editField(m, fName)
	m = typeText(m, "!")
	m = send(m, "esc")
	if m.f == nil {
		t.Fatal("esc while editing should not cancel the form")
	}
	if v := m.f.val(fName, 0); v != "A!" {
		t.Fatalf("esc should commit the edit, got %q", v)
	}
}

func TestInsertAfterSelection(t *testing.T) {
	setup(t)
	m := withItems(t, "A", "B", "C")
	m = send(m, "C", "j") // select B
	m = send(m, "o")
	m = typeText(m, "New")
	m = editField(m, fTimer)
	m = typeText(m, "1")
	m = saveFormKeys(m)
	if len(m.c.Items) != 4 || m.c.Items[2].Name != "New" {
		t.Fatalf("o should insert after the selection: %v", names(m.c.Items))
	}
}

func names(items []item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Name
	}
	return out
}

func TestBlockLayoutFits(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t,
		block{Title: "Focused Reading", Start: hm(9, 0), End: hm(10, 30)},
		block{Title: "Afternoon Work", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "Long block title here", Start: hm(13, 0), End: hm(14, 0)},
	)
	m.c.Blocks[0].SourceConfig = strings.Repeat("a very long source value ", 20)
	m.c.Blocks[0].Source = srcStatic
	m.refreshSources()

	screens := map[string]struct {
		at time.Time
		m  model
	}{
		"sleep":    {at(8, 0), m},
		"standard": {at(9, 30), m},
		"gap":      {at(10, 45), m},
		"cycler":   {at(11, 30), m},
		"done":     {at(15, 0), m},
		"extend":   {at(9, 30), send(m, "e")},
		"finish":   {at(9, 30), send(m, "f")},
		"blocks":   {at(11, 30), send(m, "C", "tab")},
		"blockdel": {at(11, 30), send(m, "C", "tab", "d")},
		"blockfrm": {at(11, 30), send(m, "C", "tab", "e")},
	}

	sizes := [][2]int{{34, 12}, {50, 16}, {80, 24}, {140, 45}}
	for name, sc := range screens {
		setClock(sc.at)
		s := sc.m
		for _, sz := range sizes {
			s.w, s.h = sz[0], sz[1]
			if s.f != nil {
				s.f.resize(s.w)
			}
			v := s.View()
			lines := strings.Split(v, "\n")
			if len(lines) != sz[1] {
				t.Errorf("%s %v: %d lines", name, sz, len(lines))
			}
			for i, l := range lines {
				if lipgloss.Width(l) > sz[0] {
					t.Errorf("%s %v: line %d is %d wide", name, sz, i, lipgloss.Width(l))
				}
			}
		}
	}
}

func TestExtendStepsAndWarning(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t,
		block{Title: "A", Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "B", Start: hm(12, 0), End: hm(12, 15)},
	)
	m = send(m, "e")
	if m.ext == nil || extendSteps[m.ext.step] != 10 {
		t.Fatal("extend should open at 10m")
	}
	m = send(m, "h")
	if extendSteps[m.ext.step] != 5 {
		t.Fatal("left should step down to 5m")
	}
	m = send(m, "l", "l")
	if extendSteps[m.ext.step] != 20 {
		t.Fatalf("right should step up, got %d", extendSteps[m.ext.step])
	}

	// 20m past 12:00 swallows B, and the control says so
	if !strings.Contains(m.View(), "Will cancel B") {
		t.Fatalf("want the cancellation warning:\n%s", m.View())
	}
	m = send(m, "enter")
	bs := m.s.dayBlocks(m.c)
	if bs[0].end != hm(12, 20) || !bs[1].cancelled {
		t.Fatalf("commit should extend A and cancel B: %+v", bs)
	}

	// esc leaves the day alone
	m = send(m, "e", "l", "esc")
	if m.ext != nil {
		t.Fatal("esc should close the control")
	}
	if m.s.dayBlocks(m.c)[0].end != hm(12, 20) {
		t.Fatal("esc should not commit")
	}
}

func TestFinishBlockEarlyAndFinishDay(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t,
		block{Title: "A", Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "B", Start: hm(12, 0), End: hm(13, 0)},
	)
	m = send(m, "f")
	if m.prompt == nil || !strings.Contains(m.prompt.prompt, "Finish A early?") {
		t.Fatalf("want the finish prompt, got %+v", m.prompt)
	}
	m = send(m, "y")
	bs := m.s.dayBlocks(m.c)
	if bs[0].end != hm(11, 30) || bs[1].start != hm(11, 30) {
		t.Fatalf("finishing early should move the boundary: %+v", bs)
	}
	if ph, b, _ := m.phase(); ph != phaseBlock || b.Title != "B" {
		t.Fatalf("B should be running, got %d %q", ph, b.Title)
	}

	// F ends the whole day in place, keeping the log for the Day Done count
	m = send(m, "F", "y")
	if !m.s.Finished {
		t.Fatal("F should finish the day")
	}
	m = send(m, " ")
	if ph, _, _ := m.phase(); ph != phaseDone {
		t.Fatalf("want Day Done, got %d", ph)
	}
	if !strings.Contains(m.View(), "Day is done!") {
		t.Fatalf("want the day-done copy:\n%s", m.View())
	}
}

func TestStartNextFromGap(t *testing.T) {
	setClock := setup(t)
	setClock(at(10, 45))
	m := withBlocks(t,
		block{Title: "A", Start: hm(9, 0), End: hm(10, 0)},
		block{Title: "B", Start: hm(11, 0), End: hm(12, 0)},
	)
	if ph, _, _ := m.phase(); ph != phaseGap {
		t.Fatalf("want a gap, got %d", ph)
	}
	m = send(m, "f", "y")
	bs := m.s.dayBlocks(m.c)
	if bs[0].end != hm(10, 0) {
		t.Fatalf("the block before the gap should keep its end: %+v", bs[0])
	}
	if bs[1].start != hm(10, 45) {
		t.Fatalf("B should start now: %+v", bs[1])
	}
}

func TestBlockTrailStyling(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t,
		block{Title: "Past", Start: hm(9, 0), End: hm(10, 0)},
		block{Title: "Now", Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "Soon", Type: blkCycler, Start: hm(13, 0), End: hm(14, 0)},
	)
	cs, cur := m.blockCrumbs()
	if len(cs) != 3 || cur != 1 {
		t.Fatalf("want three crumbs anchored on the current one: %d %d", len(cs), cur)
	}
	want := []string{"●0900 Past", "▶1100 Now", "◷1300 Soon"}
	for i, w := range want {
		if cs[i].text != w {
			t.Errorf("crumb %d: want %q, got %q", i, w, cs[i].text)
		}
	}

	// a cancelled block is struck through rather than dropped
	m.s.endBlockAt(m.c, 1, hm(14, 30))
	cs, _ = m.blockCrumbs()
	if len(cs) != 3 {
		t.Fatal("a cancelled block stays on the trail")
	}
	if !cs[2].style.GetStrikethrough() {
		t.Fatal("a cancelled block should be struck through")
	}
}

func TestExplicitDaysColumn(t *testing.T) {
	got := explicitDays(weekdayDays())
	if got != "Mo Tu We Th Fr      " {
		t.Fatalf("days column should hold its width: %q", got)
	}
	if lipgloss.Width(got) != colDays {
		t.Fatalf("want %d columns, got %d", colDays, lipgloss.Width(got))
	}
	if !slices.Equal(normalizeDays([]int{5, 1}), []int{1, 5}) {
		t.Fatal("sanity")
	}
}

func TestClockHelpers(t *testing.T) {
	if got := fmtClock(hm(9, 5)); got != "09:05" {
		t.Fatalf("fmtClock: %q", got)
	}
	if got := fmtClockCompact(hm(9, 5)); got != "0905" {
		t.Fatalf("fmtClockCompact: %q", got)
	}
	if _, ok := parseClock("12", "90"); ok {
		t.Fatal("12:90 should not parse")
	}
	if _, ok := parseClock("", "30"); ok {
		t.Fatal("an empty cell should not parse")
	}
	if v, ok := parseClock("23", "59"); !ok || v != hm(23, 59) {
		t.Fatalf("23:59 should parse: %d %v", v, ok)
	}
	for _, c := range []struct {
		mins int
		want string
	}{{5, "5m"}, {60, "1h"}, {70, "1h10m"}, {120, "2h"}} {
		if got := fmtDur(c.mins); got != c.want {
			t.Errorf("fmtDur(%d): want %q, got %q", c.mins, c.want, got)
		}
	}
}

func TestSleepStartsFirstBlockEarly(t *testing.T) {
	setClock := setup(t)
	setClock(at(8, 0))
	m := withBlocks(t,
		block{Title: "A", Start: hm(9, 0), End: hm(10, 0)},
		block{Title: "B", Start: hm(10, 0), End: hm(11, 0)},
	)
	if ph, _, _ := m.phase(); ph != phaseSleep {
		t.Fatalf("want sleep, got %d", ph)
	}
	m = send(m, "f", "y")
	if bs := m.s.dayBlocks(m.c); bs[0].start != hm(8, 0) {
		t.Fatalf("A should start now: %+v", bs[0])
	}
	if ph, b, _ := m.phase(); ph != phaseBlock || b.Title != "A" {
		t.Fatalf("A should be running, got %d %q", ph, b.Title)
	}
}

func TestDayDoneAndFreeTimeAreQuiet(t *testing.T) {
	setClock := setup(t)
	setClock(at(15, 0))
	m := withBlocks(t, block{Title: "A", Start: hm(9, 0), End: hm(10, 0)})
	if ph, _, _ := m.phase(); ph != phaseDone {
		t.Fatalf("want done, got %d", ph)
	}
	// F has nothing left to finish, and must not strand the day
	before := m.s.Finished
	m = send(m, "F")
	if m.prompt != nil || m.s.Finished != before {
		t.Fatal("F should be inert once the day is done")
	}
	m = send(m, "f")
	if m.prompt != nil {
		t.Fatal("f should be inert once the day is done")
	}
}

func TestLeadingZeroIsDimmed(t *testing.T) {
	// the slab '0' is six columns wide, plus the gap before the next glyph
	if got := dimPrefix(fontSlab, "09:05"); got != 7 {
		t.Fatalf("a padded hour should dim its leading zero, got %d", got)
	}
	if got := dimPrefix(fontSlab, "19:05"); got != 0 {
		t.Fatalf("a real digit should not be dimmed, got %d", got)
	}
	// and the art still renders the whole time either way
	rows := clockArt("09:05", textStyle, 80, 24)
	if len(rows) != fontSlab.rows {
		t.Fatalf("want %d rows, got %d", fontSlab.rows, len(rows))
	}
	if !strings.Contains(rows[0], fontSlab.glyphs['9'][0]) {
		t.Fatal("the rest of the clock should still be there")
	}
}

func TestEndTimeWarnsInFinalStretch(t *testing.T) {
	end := hm(10, 30)
	for _, c := range []struct {
		now  int
		want bool
	}{
		{hm(10, 24), false},
		{hm(10, 25), true},
		{hm(10, 29), true},
		{hm(10, 30), false},
		{hm(10, 31), false},
	} {
		if got := inFinalStretch(c.now, end); got != c.want {
			t.Errorf("%s: want %v, got %v", fmtClock(c.now), c.want, got)
		}
	}
}
