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

// finishing early ends the block and opens free time; it never drags the next
// block back, so a configured gap can't be swallowed
func TestFinishEarlyOpensFreeTime(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t,
		block{Title: "A", Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "B", Start: hm(12, 0), End: hm(12, 30)},
	)
	m.s.endBlockAt(m.c, 0, hm(11, 30))

	bs := m.s.dayBlocks(m.c)
	if bs[0].end != hm(11, 30) {
		t.Fatalf("A should end now: %+v", bs[0])
	}
	if bs[1].start != hm(12, 0) || bs[1].end != hm(12, 30) {
		t.Fatalf("B should be untouched: %+v", bs[1])
	}
	if ph, _, _ := m.phase(); ph != phaseGap {
		t.Fatalf("the time between them is free, got phase %d", ph)
	}
}

// a long lunch between two blocks survives finishing the morning early
func TestFinishEarlyKeepsAConfiguredGap(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 0))
	m := withBlocks(t,
		block{Title: "Morning", Start: hm(9, 0), End: hm(12, 0)},
		block{Title: "Afternoon", Start: hm(14, 0), End: hm(18, 0)},
	)
	m = send(m, "f", "y")

	bs := m.s.dayBlocks(m.c)
	if bs[1].start != hm(14, 0) || bs[1].end != hm(18, 0) {
		t.Fatalf("the lunch gap must survive: %+v", bs[1])
	}
	if ph, b, _ := m.phase(); ph != phaseGap || b.Title != "Afternoon" {
		t.Fatalf("want free time until Afternoon, got %d %q", ph, b.Title)
	}
	// and the gap can still be cut short deliberately
	m = send(m, "f", "y")
	if bs := m.s.dayBlocks(m.c); bs[1].start != hm(11, 0) {
		t.Fatalf("f from the gap should pull Afternoon forward: %+v", bs[1])
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
	if bs[0].end != hm(11, 30) || bs[1].start != hm(12, 0) {
		t.Fatalf("A ends now and B keeps its start: %+v", bs)
	}
	if ph, b, _ := m.phase(); ph != phaseGap || b.Title != "B" {
		t.Fatalf("free time until B, got %d %q", ph, b.Title)
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

// a prompt or control answered after the clock has moved on must act on the
// block it named, not on whatever became current in the meantime
func TestLatePromptActsOnTheNamedBlock(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 59))
	m := withBlocks(t,
		block{Title: "A", Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "B", Start: hm(12, 0), End: hm(13, 0)},
	)
	m = send(m, "f")
	if m.prompt == nil || !strings.Contains(m.prompt.prompt, "Finish A early?") {
		t.Fatalf("want the prompt against A, got %+v", m.prompt)
	}
	setClock(at(12, 0))
	m = send(m, "y")

	bs := m.s.dayBlocks(m.c)
	if bs[1].cancelled {
		t.Fatal("answering late must not destroy B")
	}
	if bs[0].end != hm(12, 0) {
		t.Fatalf("A ran out on its own, so it is finished rather than stretched: %+v", bs[0])
	}
}

// adjacent blocks hand over with no gap between them, so a control left open
// across the boundary must not resurrect the block that just ended — nor, which
// is worse, quietly extend the one that replaced it
func TestExtendDiesWhenItsBlockHandsOver(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 59))
	m := withBlocks(t,
		block{Title: "A", Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "B", Start: hm(12, 0), End: hm(13, 0)},
	)
	m = send(m, "e") // opens on A at 10m
	setClock(at(12, 0))
	m = send(m, "enter")

	if m.ext != nil {
		t.Fatal("the control should have died with A")
	}
	bs := m.s.dayBlocks(m.c)
	if bs[0].end != hm(12, 0) {
		t.Fatalf("A ended on its own and must not be resurrected: %+v", bs[0])
	}
	if bs[1].start != hm(12, 0) || bs[1].end != hm(13, 0) {
		t.Fatalf("B must be left exactly as it was: %+v", bs[1])
	}
	if ph, b, _ := m.phase(); ph != phaseBlock || b.Title != "B" {
		t.Fatalf("B should be running, got %d %q", ph, b.Title)
	}
}

func TestExtendIsVisibleInACyclerBlock(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t,
		block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "After", Start: hm(12, 0), End: hm(12, 15)},
	)
	m = send(m, "e")
	if m.ext == nil {
		t.Fatal("e should open the control in a cycler block")
	}
	if v := m.View(); !strings.Contains(v, "Extend by") {
		t.Fatalf("the control must be visible, not a hidden modal:\n%s", v)
	}
	m = send(m, "l", "l") // 20m, which swallows After
	if v := m.View(); !strings.Contains(v, "Will cancel After") {
		t.Fatalf("the warning must be visible too:\n%s", v)
	}
	m = send(m, "esc")
	if m.ext != nil {
		t.Fatal("esc should close it")
	}
}

// a cycler prompt raised inside a block must still be answerable once the
// block's clock end has passed
func TestCyclerPromptSurvivesABlockEnding(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 59))
	m := withBlocks(t,
		block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "After", Start: hm(12, 0), End: hm(13, 0)},
	)
	m = send(m, "c")
	if m.prompt == nil || m.prompt.kind != confirmComplete {
		t.Fatalf("want the complete prompt, got %+v", m.prompt)
	}
	setClock(at(12, 0))
	m = send(m, "y")
	if len(m.s.Log) != 1 {
		t.Fatalf("the answer must not be swallowed: %+v", m.s.Log)
	}
}

func TestReEnteringATimeFieldStartsOnTheHour(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t, block{Title: "A", Start: hm(11, 0), End: hm(12, 0)})
	m = send(m, "C", "tab", "e")
	m = editField(m, fEnd)
	m = send(m, "enter") // to the minute cell
	if m.f.cell != 1 {
		t.Fatalf("setup: want the minute cell, got %d", m.f.cell)
	}
	m = send(m, "enter") // commits the line
	if m.f.editing {
		t.Fatal("setup: should have left edit mode")
	}
	m = send(m, "enter") // back in
	if m.f.cell != 0 {
		t.Fatalf("re-entering should land on the hour, got cell %d", m.f.cell)
	}
}

func TestInvalidTimeSaveSaysWhy(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t, block{Title: "A", Start: hm(11, 0), End: hm(12, 0)})
	m = send(m, "C", "tab", "e")
	m = editField(m, fEnd)
	m = send(m, "backspace", "backspace")
	m = saveFormKeys(m)
	if m.scr != scrForm {
		t.Fatal("save should not have gone through")
	}
	if m.f.err == "" {
		t.Fatal("a refused save should say why rather than doing nothing")
	}
}

func TestRolloverClosesAnOpenControl(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t, block{Title: "A", Start: hm(11, 0), End: hm(12, 0)})
	m = send(m, "e")
	if m.ext == nil {
		t.Fatal("setup: want an open control")
	}
	// the poll reads the tick's own time, so the tick has to carry the new day
	next := time.Date(2026, 10, 22, 11, 30, 0, 0, time.Local)
	setClock(next)
	m.lastTick = next.Add(-time.Second)
	m.lastPoll = next.Add(-pollEvery - time.Second)
	upd, _ := m.Update(tickMsg(next))
	m = upd.(model)
	if m.s.Date != logicalDate(next) {
		t.Fatalf("setup: the day should have rolled, got %q", m.s.Date)
	}
	if m.ext != nil {
		t.Fatal("a new day should not leave a control open against yesterday")
	}
}

// finishing a cycler block by hand must not let the overrun watch grab it back
func TestFinishingACyclerBlockDoesNotReHold(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	var notes []string
	notifyTextFn = func(s string) { notes = append(notes, s) }
	t.Cleanup(func() { notifyTextFn = notifyText })

	m := withBlocks(t,
		block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "After", Start: hm(12, 0), End: hm(13, 0)},
	)
	m = send(m, " ") // timer running
	m = send(m, "f", "y")
	if m.s.Held != "" {
		t.Fatal("finishing early should not hold")
	}

	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)
	if m.s.Held != "" {
		t.Fatalf("the poll must not re-hold a block that was finished by hand")
	}
	if len(notes) != 0 {
		t.Fatalf("and must not notify about it: %v", notes)
	}
	if ph, b, _ := m.phase(); ph != phaseGap || b.Title != "After" {
		t.Fatalf("free time until After, got %d %q", ph, b.Title)
	}
}

// extending a held block gives it more time, so it is no longer overrunning
func TestExtendingAHeldBlockReleasesIt(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	notifyTextFn = func(string) {}
	t.Cleanup(func() { notifyTextFn = notifyText })

	m := withBlocks(t,
		block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "After", Start: hm(12, 0), End: hm(13, 0)},
	)
	m = send(m, " ")
	setClock(at(12, 10))
	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)
	if m.s.Held == "" {
		t.Fatal("setup: want a held block")
	}

	// a held block is already past its end, so the step is measured from now:
	// one step right is 20m, giving 12:10 + 20m
	m = send(m, "e", "l", "enter")
	if m.s.Held != "" {
		t.Fatal("an extension past now means the block is running again")
	}
	if bs := m.s.dayBlocks(m.c); bs[0].end != hm(12, 30) {
		t.Fatalf("want an end of 12:30, got %+v", bs[0])
	}
	if !strings.Contains(m.View(), "task text") {
		t.Fatalf("the overrun banner should be gone:\n%s", m.View())
	}
}

func TestHeldBannerShowsTodaysEnd(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	notifyTextFn = func(string) {}
	t.Cleanup(func() { notifyTextFn = notifyText })

	m := withBlocks(t,
		block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "After", Start: hm(12, 0), End: hm(13, 0)},
	)
	// push the end out to 12:30, then overrun that
	m = send(m, " ", "e", "l", "l", "enter")
	if bs := m.s.dayBlocks(m.c); bs[0].end != hm(12, 30) {
		t.Fatalf("setup: want 12:30, got %+v", bs[0])
	}
	setClock(at(12, 40))
	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)
	if m.s.Held == "" {
		t.Fatal("setup: want a held block")
	}
	v := m.View()
	if !strings.Contains(v, "Block ended at 12:30") {
		t.Fatalf("the banner should report today's end, not the configured one:\n%s", v)
	}
}

func TestSleepRendersItsPrompt(t *testing.T) {
	setClock := setup(t)
	setClock(at(7, 0))
	m := withBlocks(t, block{Title: "A", Start: hm(9, 0), End: hm(10, 0)})
	if ph, _, _ := m.phase(); ph != phaseSleep {
		t.Fatalf("want sleep, got %d", ph)
	}
	m = send(m, "F")
	if m.prompt == nil {
		t.Fatal("setup: want a prompt")
	}
	if v := m.View(); !strings.Contains(v, "Finish day?") {
		t.Fatalf("an open prompt must be visible before it can be answered:\n%s", v)
	}
}

func TestFinishDayStopsTheCyclerTimer(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	fired := 0
	notifyFn = func(string) { fired++ }
	t.Cleanup(func() { notifyFn = notifyDone })

	m := withBlocks(t, block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)})
	m = send(m, " ")
	if m.s.Timer != timerGoing {
		t.Fatal("setup: want a running timer")
	}
	m = send(m, "F", "y")
	if m.s.Timer == timerGoing {
		t.Fatal("finishing the day should stop the timer")
	}
	m = send(m, " ") // dismiss the finish screen
	m = runClock(m, 65)
	if fired != 0 {
		t.Fatalf("a finished day must not fire a completion notification, got %d", fired)
	}
}

func TestExtendControlDiesWithItsBlock(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 59))
	m := withBlocks(t,
		block{Title: "A", Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "B", Start: hm(13, 0), End: hm(14, 0)},
	)
	m = send(m, "e")
	if m.ext == nil {
		t.Fatal("setup: want an open control")
	}

	// the block ends while the control is open, so the day moves into the gap
	setClock(at(12, 1))
	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)
	if m.ext != nil {
		t.Fatal("the control should not outlive its block")
	}
	if ph, _, _ := m.phase(); ph != phaseGap {
		t.Fatalf("want a gap, got %d", ph)
	}
	// and the gap's own keys work rather than being swallowed
	m = send(m, "f")
	if m.prompt == nil {
		t.Fatal("f should work once the control is gone")
	}
}

func TestExtendIsVisibleOnAnEmptyCycler(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	c := &config{}
	c.Items = append(c.Items, item{
		ID: newID(), Name: "Off today", Seconds: 60, ActiveDays: []int{},
		Source: srcStatic, SourceConfig: "x",
	})
	c.Blocks = append(c.Blocks, block{
		ID: newID(), Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0),
		ActiveDays: allDays(), Color: defaultBlockColor, Source: srcStatic,
	})
	c.save()
	m := newModel()
	if m.s.Current != "" {
		t.Fatal("setup: want an empty cycler")
	}
	m = send(m, "e")
	if v := m.View(); !strings.Contains(v, "Extend by") {
		t.Fatalf("the control must be visible here too:\n%s", v)
	}
}

func TestDayDoneRendersItsPrompt(t *testing.T) {
	setClock := setup(t)
	setClock(at(9, 58))
	m := withBlocks(t, block{Title: "A", Start: hm(9, 0), End: hm(10, 0)})
	m = send(m, "F")
	if m.prompt == nil {
		t.Fatal("setup: want a prompt")
	}
	setClock(at(10, 1))
	if ph, _, _ := m.phase(); ph != phaseDone {
		t.Fatalf("want day done, got %d", ph)
	}
	if v := m.View(); !strings.Contains(v, "Finish day?") {
		t.Fatalf("a live prompt must stay visible across a phase change:\n%s", v)
	}
}

func TestFinishingACyclerBlockPausesItsTimer(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	fired := 0
	notifyFn = func(string) { fired++ }
	t.Cleanup(func() { notifyFn = notifyDone })

	m := withBlocks(t,
		block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "After", Start: hm(12, 0), End: hm(13, 0)},
	)
	m = send(m, " ")
	m = send(m, "f", "y")
	if m.s.Timer == timerGoing {
		t.Fatal("taking the cycler off screen should stop its timer")
	}
	m = runClock(m, 65)
	if fired != 0 {
		t.Fatalf("no notification should fire with the cycler off screen, got %d", fired)
	}
}

func TestEditingAHeldBlockOffTodayClearsTheHold(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	notifyTextFn = func(string) {}
	t.Cleanup(func() { notifyTextFn = notifyText })

	m := withBlocks(t, block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)})
	m = send(m, " ")
	setClock(at(12, 10))
	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)
	if m.s.Held == "" {
		t.Fatal("setup: want a held block")
	}

	// take it off today's schedule entirely
	m = send(m, "C", "tab", "e")
	m = toField(m, fDays)
	m = send(m, "right", "right")
	for range 7 {
		m = send(m, " ", "right")
	}
	m = saveFormKeys(m)
	if m.scr != scrConfig {
		t.Fatalf("save failed: %+v", m.f)
	}
	if m.s.Held != "" {
		t.Fatal("a block that no longer runs today cannot stay held")
	}
}

func TestExtendWarningSurvivesAShortPanel(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t,
		block{Title: "A", Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "B", Start: hm(12, 0), End: hm(12, 15)},
	)
	m = send(m, "e", "l") // 20m, which swallows B
	if rows := m.extendRows(contentWidth(80), 2); len(rows) != 2 {
		t.Fatalf("with room for two, the warning gets its own row: %d", len(rows))
	}
	rows := m.extendRows(contentWidth(80), 1)
	if len(rows) != 1 {
		t.Fatalf("want one row, got %d", len(rows))
	}
	if !strings.Contains(rows[0], "Will cancel B") {
		t.Fatalf("a destructive step must warn even when the panel is short: %q", rows[0])
	}
}

// a prompt the next keypress will answer has to be legible at every size the
// app claims to support, even when the clock art has to go to make room
func TestPromptsAndControlsSurviveMinimumSize(t *testing.T) {
	setClock := setup(t)
	m := withBlocks(t,
		block{Title: "Focused Reading", Start: hm(9, 0), End: hm(10, 0)},
		block{Title: "Short", Start: hm(10, 0), End: hm(10, 20)},
		block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)},
	)
	sizes := [][2]int{{34, 12}, {50, 16}, {80, 24}}

	cases := []struct {
		name string
		at   time.Time
		keys []string
		want string
	}{
		{"block finish prompt", at(9, 30), []string{"f"}, "Finish Focused"},
		{"block extend control", at(9, 30), []string{"e"}, "Extend by"},
		{"extend warning", at(9, 30), []string{"e", "l", "l"}, "Will cancel Short"},
		{"finish day prompt", at(9, 30), []string{"F"}, "Finish day?"},
		{"gap prompt", at(10, 30), []string{"f"}, "Start Cycle"},
		{"sleep prompt", at(8, 0), []string{"F"}, "Finish day?"},
		{"cycler extend", at(11, 30), []string{"e"}, "Extend by"},
		{"cycler prompt", at(11, 30), []string{"c"}, "Complete early?"},
		{"skip prompt", at(11, 30), []string{"s"}, "Skip item?"},
	}

	for _, c := range cases {
		setClock(c.at)
		for _, sz := range sizes {
			s := m
			s.w, s.h = sz[0], sz[1]
			s = send(s, c.keys...)
			v := s.View()
			if !strings.Contains(v, c.want) {
				t.Errorf("%s at %v: %q is not on screen:\n%s", c.name, sz, c.want, v)
			}
			for i, l := range strings.Split(v, "\n") {
				if lipgloss.Width(l) > sz[0] {
					t.Errorf("%s at %v: line %d is %d wide", c.name, sz, i, lipgloss.Width(l))
				}
			}
		}
	}
}

func TestEditingABlockWindowDoesNotFakeAnOverrun(t *testing.T) {
	setClock := setup(t)
	setClock(at(10, 45))
	var notes []string
	notifyTextFn = func(s string) { notes = append(notes, s) }
	t.Cleanup(func() { notifyTextFn = notifyText })

	m := withBlocks(t, block{Title: "Cycle", Type: blkCycler, Start: hm(10, 0), End: hm(11, 0)})
	m = send(m, " ") // timer running inside the block

	// push the window later; the block has not ended, it has moved
	m = send(m, "C", "tab", "e")
	m = editField(m, fStart)
	m = send(m, "backspace", "backspace")
	m = typeText(m, "11")
	m = send(m, "tab", "backspace", "backspace")
	m = typeText(m, "00")
	m = toField(m, fEnd)
	m = send(m, "enter", "backspace", "backspace")
	m = typeText(m, "12")
	m = saveFormKeys(m)
	if m.scr != scrConfig {
		t.Fatalf("save failed: %+v", m.f)
	}
	m = send(m, "esc")

	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)
	if m.s.Held != "" {
		t.Fatal("a window that moved is not a block that ended")
	}
	if len(notes) != 0 {
		t.Fatalf("and must not notify: %v", notes)
	}
}

// the day now turns at midnight, which is exactly when a timer is most likely
// to be mid-run; holding the roll off would leave every block decision
// resolving against yesterday
func TestMidnightRollsOverUnderARunningTimer(t *testing.T) {
	setClock := setup(t)
	setClock(time.Date(2026, 10, 21, 23, 59, 0, 0, time.Local))
	m := withItems(t, "A")
	m = send(m, " ")
	if m.s.Timer != timerGoing {
		t.Fatal("setup: want a running timer")
	}

	next := time.Date(2026, 10, 22, 0, 1, 0, 0, time.Local)
	setClock(next)
	m.lastTick = next.Add(-time.Second)
	m.lastPoll = next.Add(-pollEvery - time.Second)
	upd, _ := m.Update(tickMsg(next))
	m = upd.(model)

	if m.s.Date != logicalDate(next) {
		t.Fatalf("a running timer must not hold the day on yesterday: %q", m.s.Date)
	}
	if m.s.Timer == timerGoing {
		t.Fatal("the new day starts with a fresh, unstarted timer")
	}
}

func TestOverrunWatchIsQuietWhileConfiguring(t *testing.T) {
	setClock := setup(t)
	setClock(at(10, 45))
	var notes []string
	notifyTextFn = func(s string) { notes = append(notes, s) }
	t.Cleanup(func() { notifyTextFn = notifyText })

	m := withBlocks(t, block{Title: "Cycle", Type: blkCycler, Start: hm(10, 0), End: hm(11, 0)})
	m = send(m, " ")
	m = send(m, "C", "tab", "e") // sitting in the block form

	// the window the user is halfway through editing lapses under them
	setClock(at(11, 5))
	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)

	if m.s.Held != "" {
		t.Fatal("an edit in progress must not trip the overrun watch")
	}
	if len(notes) != 0 {
		t.Fatalf("and must not notify mid-edit: %v", notes)
	}
}

func TestExtendStepperShowsTheEffectiveStep(t *testing.T) {
	setClock := setup(t)
	setClock(at(23, 30))
	m := withBlocks(t, block{
		Title: "Late", Start: hm(23, 0), End: hm(23, 50),
		ActiveDays: allDays(),
	})
	m = send(m, "e")
	for range len(extendSteps) {
		m = send(m, "l")
	}
	// 23:50 + 2h clamps to 23:59, which is nine minutes, not two hours
	rows := m.extendRows(contentWidth(80), 2)
	if len(rows) == 0 || !strings.Contains(rows[0], "9m") {
		t.Fatalf("the stepper should show what the commit will actually do: %q", rows)
	}
	if strings.Contains(rows[0], "2h") {
		t.Fatalf("and not a step it cannot take: %q", rows[0])
	}
	m = send(m, "enter")
	if bs := m.s.dayBlocks(m.c); bs[0].end != minsPerDay-1 {
		t.Fatalf("want a clamped end, got %+v", bs[0])
	}
}

// an overrun must survive a config visit that spans the block's end: the old
// transition detector lost the evidence and left the timer counting down with
// nothing on screen to answer it
func TestOverrunSurvivesAConfigVisit(t *testing.T) {
	setClock := setup(t)
	setClock(at(10, 56))
	var notes []string
	notifyTextFn = func(s string) { notes = append(notes, s) }
	t.Cleanup(func() { notifyTextFn = notifyText })

	m := withBlocks(t,
		block{Title: "Deep Work", Type: blkCycler, Start: hm(9, 0), End: hm(11, 0)},
		block{Title: "Lunch", Start: hm(11, 0), End: hm(12, 0)},
	)
	m = send(m, " ")
	m = send(m, "C") // just looking
	setClock(at(11, 2))
	m = send(m, "esc")

	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)
	if m.s.Held == "" {
		t.Fatal("the overrun must still be detected after a config visit")
	}
	if len(notes) != 1 {
		t.Fatalf("want one notification, got %v", notes)
	}
	if ph, b, _ := m.phase(); ph != phaseBlock || b.Title != "Deep Work" {
		t.Fatalf("the held block should be current, got %d %q", ph, b.Title)
	}
}

func TestOverrunSurvivesARestart(t *testing.T) {
	setClock := setup(t)
	setClock(at(10, 56))
	notifyTextFn = func(string) {}
	t.Cleanup(func() { notifyTextFn = notifyText })

	m := withBlocks(t,
		block{Title: "Deep Work", Type: blkCycler, Start: hm(9, 0), End: hm(11, 0)},
		block{Title: "Lunch", Start: hm(11, 0), End: hm(12, 0)},
	)
	m = send(m, " ")
	if m.s.Running == "" {
		t.Fatal("setup: the running block should be recorded")
	}
	qm, _ := m.quit()
	m = qm.(model)

	setClock(at(11, 2))
	m = newModel()
	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)
	if m.s.Held == "" {
		t.Fatal("a restart across the boundary must not lose the overrun")
	}
}

// resolved windows, not configured ones, decide what is running
func TestResolvedBlocksStayInOrder(t *testing.T) {
	setClock := setup(t)
	setClock(at(10, 30))
	m := withBlocks(t,
		block{Title: "A", Start: hm(9, 0), End: hm(10, 0)},
		block{Title: "B", Start: hm(11, 0), End: hm(12, 0)},
	)
	// pull B back into the gap, so its resolved start is 10:30
	m = send(m, "f", "y")
	if bs := m.s.dayBlocks(m.c); bs[1].start != hm(10, 30) {
		t.Fatalf("setup: want B at 10:30, got %+v", bs[1])
	}

	// now move A late. the overlap check passes, because it compares against
	// B's configured 11:00 rather than the 10:30 it actually has today.
	m = send(m, "C", "tab", "e")
	m = editField(m, fStart)
	m = send(m, "backspace", "backspace")
	m = typeText(m, "10")
	m = send(m, "tab", "backspace", "backspace")
	m = typeText(m, "45")
	m = toField(m, fEnd)
	m = send(m, "enter", "backspace", "backspace")
	m = typeText(m, "10")
	m = send(m, "tab", "backspace", "backspace")
	m = typeText(m, "50")
	m = saveFormKeys(m)
	if m.scr != scrConfig {
		t.Fatalf("save failed: %+v", m.f)
	}
	m = send(m, "esc")

	bs := m.s.dayBlocks(m.c)
	for i := 1; i < len(bs); i++ {
		if bs[i].start < bs[i-1].start {
			t.Fatalf("resolved blocks must be in start order: %+v", bs)
		}
	}
	setClock(at(10, 40))
	if ph, b, _ := m.phase(); ph != phaseBlock || b.Title != "B" {
		t.Fatalf("B is running at 10:40, got phase %d %q", ph, b.Title)
	}
}

func TestNewBlockFormDoesNotCryInvalid(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t, block{Title: "A", Start: hm(11, 0), End: hm(12, 0)})
	m = send(m, "C", "tab", "a")
	if v := m.View(); strings.Contains(v, "Invalid!") {
		t.Fatalf("an untouched field is not an error yet:\n%s", v)
	}
	if m.f.saveReady() {
		t.Fatal("but it still blocks the save")
	}
	// typing something unreadable does flag it
	m = editField(m, fStart)
	m = typeText(m, "99")
	m = send(m, "esc")
	if !strings.Contains(m.View(), "Invalid!") {
		t.Fatalf("a filled-in bad time should flag:\n%s", m.View())
	}
}

// completing a held item hands the day to the next block; the cycler goes off
// screen with it, so nothing may start counting down under the new one
func TestReleasingAHoldDoesNotStartAnInvisibleTimer(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	fired := 0
	notifyFn = func(string) { fired++ }
	notifyTextFn = func(string) {}
	t.Cleanup(func() { notifyFn = notifyDone; notifyTextFn = notifyText })

	m := withBlocks(t,
		block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "After", Start: hm(12, 0), End: hm(13, 0)},
	)
	m = send(m, " ")
	setClock(at(12, 10))
	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)
	if m.s.Held == "" {
		t.Fatal("setup: want a held block")
	}

	// let the timer finish, then space through it
	m.s.Remaining = 1
	m = runClock(m, 2)
	if m.s.Timer != timerDone {
		t.Fatalf("setup: want a finished timer, got %q", m.s.Timer)
	}
	fired = 0
	m = send(m, " ")

	if ph, b, _ := m.phase(); ph != phaseBlock || b.Title != "After" {
		t.Fatalf("the day should have moved on, got %d %q", ph, b.Title)
	}
	if m.s.Timer == timerGoing {
		t.Fatal("the cycler is off screen, so its timer must not be running")
	}
	m = runClock(m, 120)
	if fired != 0 {
		t.Fatalf("and must not fire a notification nothing can answer, got %d", fired)
	}
}

// a hold is for work in progress; if the work goes away the hold must too,
// or the day wedges on a block that already ended
func TestHoldDiesWithItsItem(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	notifyTextFn = func(string) {}
	t.Cleanup(func() { notifyTextFn = notifyText })

	m := withBlocks(t,
		block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)},
		block{Title: "After", Start: hm(12, 0), End: hm(13, 0)},
	)
	m = send(m, " ")
	setClock(at(12, 10))
	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)
	if m.s.Held == "" {
		t.Fatal("setup: want a held block")
	}

	// schedule the running item out of today, which leaves no current item
	m = send(m, "C", "e")
	m = toField(m, fDays)
	m = send(m, "right", "right")
	for range 7 {
		m = send(m, " ", "right")
	}
	m = saveFormKeys(m)
	m = send(m, "esc")
	if m.s.Current != "" {
		t.Fatalf("setup: want an empty cycler, got %q", m.s.Current)
	}

	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)
	if m.s.Held != "" {
		t.Fatal("a hold must not outlive the work it was holding for")
	}
	if ph, b, _ := m.phase(); ph != phaseBlock || b.Title != "After" {
		t.Fatalf("the day should resume, got %d %q", ph, b.Title)
	}
}

func TestASecondOverrunStillNotifies(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	var notes []string
	notifyTextFn = func(s string) { notes = append(notes, s) }
	t.Cleanup(func() { notifyTextFn = notifyText })

	m := withBlocks(t, block{Title: "Cycle", Type: blkCycler, Start: hm(11, 0), End: hm(12, 0)})
	m = send(m, " ")
	setClock(at(12, 10))
	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)
	if len(notes) != 1 {
		t.Fatalf("want the first notification, got %v", notes)
	}

	// buy twenty more minutes, then run past those too
	m = send(m, "e", "l", "enter")
	if m.s.Held != "" {
		t.Fatal("setup: the extension should have released the hold")
	}
	setClock(at(12, 40))
	m.lastPoll = m.lastTick.Add(-pollEvery)
	m = runClock(m, 1)
	if m.s.Held == "" {
		t.Fatal("setup: want a second hold")
	}
	if len(notes) != 2 {
		t.Fatalf("a second overrun is news too, got %v", notes)
	}
}

func TestStaleFormErrorClearsOnNavigation(t *testing.T) {
	setClock := setup(t)
	setClock(at(11, 30))
	m := withBlocks(t,
		block{Title: "A", Start: hm(9, 0), End: hm(10, 0)},
		block{Title: "B", Start: hm(10, 0), End: hm(11, 0)},
	)
	// edit A to collide with B
	m = send(m, "C", "tab", "e")
	m = editField(m, fEnd)
	m = send(m, "backspace", "backspace")
	m = typeText(m, "10")
	m = send(m, "tab", "backspace", "backspace")
	m = typeText(m, "30")
	m = saveFormKeys(m)
	if m.f == nil || m.f.err == "" {
		t.Fatal("setup: want an overlap error")
	}
	m = send(m, "j")
	if m.f.err != "" {
		t.Fatalf("a refusal should not linger once the user moves on: %q", m.f.err)
	}
}

// a day whose blocks all belong to other weekdays is free time, but the cycler
// can still be started by hand and runs open-ended until F or midnight
func TestOpenCyclerOnABlocklessDay(t *testing.T) {
	setClock := setup(t)
	setClock(at(10, 0)) // 2026-10-21 is a Wednesday
	m := withBlocks(t, block{
		Title: "Sunday only", Start: hm(9, 0), End: hm(10, 30),
		ActiveDays: []int{int(time.Sunday)},
	})
	if ph, _, _ := m.phase(); ph != phaseNoBlocks {
		t.Fatalf("want a blockless day, got %d", ph)
	}
	if !strings.Contains(m.View(), "FREE TIME") {
		t.Fatal("setup: want free time")
	}

	m = send(m, "c")
	ph, b, _ := m.phase()
	if ph != phaseBlock || b.Type != blkCycler {
		t.Fatalf("c should open a cycler block, got %d %+v", ph, b)
	}
	if b.start != hm(10, 0) {
		t.Fatalf("it should mark the time it started: %+v", b)
	}
	if b.end != minsPerDay {
		t.Fatalf("and have no scheduled end: %+v", b)
	}
	if v := m.View(); !strings.Contains(v, "task text") {
		t.Fatalf("the cycler should be on screen:\n%s", v)
	}

	// it runs the whole day without ever overrunning
	setClock(at(23, 30))
	if ph, _, _ := m.phase(); ph != phaseBlock {
		t.Fatalf("it should still be running at 23:30, got %d", ph)
	}
	// the cycler's own keys work inside it
	m = send(m, " ")
	if m.s.Timer != timerGoing {
		t.Fatal("space should start the timer")
	}
	// and F still ends the day
	m = send(m, "F", "y")
	if !m.s.Finished {
		t.Fatal("F should finish the day")
	}
	m = send(m, " ")
	if ph, _, _ := m.phase(); ph != phaseDone {
		t.Fatalf("want day done, got %d", ph)
	}
}

func TestOpenCyclerHasNoFinishOrExtend(t *testing.T) {
	setClock := setup(t)
	setClock(at(10, 0))
	m := withBlocks(t, block{
		Title: "Sunday only", Start: hm(9, 0), End: hm(10, 30),
		ActiveDays: []int{int(time.Sunday)},
	})
	m = send(m, "c")
	m = send(m, "e")
	if m.ext != nil {
		t.Fatal("there is no scheduled end to extend")
	}
	m = send(m, "f")
	if m.prompt != nil {
		t.Fatal("and nothing to finish early")
	}
}

func TestOpenCyclerDiesAtMidnight(t *testing.T) {
	setClock := setup(t)
	setClock(time.Date(2026, 10, 21, 23, 0, 0, 0, time.Local))
	m := withBlocks(t, block{
		Title: "Sunday only", Start: hm(9, 0), End: hm(10, 30),
		ActiveDays: []int{int(time.Sunday)},
	})
	m = send(m, "c")
	if !m.s.OpenCycler {
		t.Fatal("setup: want an open cycler")
	}

	next := time.Date(2026, 10, 22, 0, 1, 0, 0, time.Local)
	setClock(next)
	m.lastTick = next.Add(-time.Second)
	m.lastPoll = next.Add(-pollEvery - time.Second)
	upd, _ := m.Update(tickMsg(next))
	m = upd.(model)
	if m.s.OpenCycler {
		t.Fatal("it should not carry into the new day")
	}
}

func TestOpenCyclerOnlyOnABlocklessDay(t *testing.T) {
	setClock := setup(t)
	setClock(at(10, 45))
	m := withBlocks(t,
		block{Title: "A", Start: hm(9, 0), End: hm(10, 0)},
		block{Title: "B", Start: hm(11, 0), End: hm(12, 0)},
	)
	if ph, _, _ := m.phase(); ph != phaseGap {
		t.Fatalf("setup: want a bounded gap, got %d", ph)
	}
	m = send(m, "c")
	if m.s.OpenCycler {
		t.Fatal("a gap between real blocks is not a blockless day")
	}
}
