package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var testNow = time.Date(2026, 10, 21, 14, 0, 0, 0, time.Local)

// isolates config and pins the clock; the returned func moves the clock
func setup(t *testing.T) func(time.Time) {
	prevDir := configDirFn
	dir := t.TempDir()
	configDirFn = func() string { return dir }
	now := testNow
	nowFn = func() time.Time { return now }
	notifyFn = func(string) {}
	t.Cleanup(func() { nowFn = time.Now; notifyFn = notifyDone; configDirFn = prevDir })
	return func(n time.Time) { now = n }
}

func key(s string) tea.KeyMsg {
	switch s {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func send(m model, keys ...string) model {
	for _, k := range keys {
		next, _ := m.Update(key(k))
		m = next.(model)
	}
	return m
}

func typeText(m model, s string) model {
	for _, r := range s {
		m = send(m, string(r))
	}
	return m
}

// runs the clock forward in one-second ticks, below the sleep threshold
func runClock(m model, secs int) model {
	t := m.lastTick
	for i := 0; i < secs; i++ {
		t = t.Add(time.Second)
		next, _ := m.Update(tickMsg(t))
		m = next.(model)
	}
	return m
}

func withItems(t *testing.T, names ...string) model {
	c := &config{}
	for _, n := range names {
		c.Items = append(c.Items, item{ID: newID(), Name: n, Seconds: 60, Source: srcStatic, SourceConfig: n + " text"})
	}
	c.save()
	return newModel()
}

func TestFirstRunToCycler(t *testing.T) {
	setup(t)
	m := newModel()
	if m.scr != scrFirstRun {
		t.Fatalf("want first run, got %d", m.scr)
	}
	m = send(m, "C")
	if m.scr != scrConfig {
		t.Fatalf("want config")
	}
	m = send(m, "esc")
	if m.scr != scrFirstRun {
		t.Fatalf("empty config esc should return to first run")
	}

	m = send(m, "c", "a")
	m = typeText(m, "PHB")
	m = send(m, "tab")
	m = typeText(m, "1")
	m = send(m, "tab")
	m = typeText(m, "30")
	m = send(m, "tab", "tab", "tab", "right", "right", "right", "tab")
	m = typeText(m, "hello")
	m = send(m, "tab", "enter")
	if m.scr != scrConfig || len(m.c.Items) != 1 {
		t.Fatalf("save failed: scr %d items %d", m.scr, len(m.c.Items))
	}
	it := m.c.Items[0]
	if it.Name != "PHB" || it.Seconds != 90 || it.Source != srcStatic || it.SourceConfig != "hello" {
		t.Fatalf("bad item %+v", it)
	}
	if m.s.Current != it.ID || m.s.Remaining != 90 {
		t.Fatalf("first item should become current: %+v", m.s)
	}
	m = send(m, "esc")
	if m.scr != scrCycler {
		t.Fatalf("want cycler")
	}

	// persisted
	m2 := newModel()
	if len(m2.c.Items) != 1 || m2.scr != scrCycler {
		t.Fatalf("not persisted")
	}
}

func TestTimerRunsAndAdvances(t *testing.T) {
	setup(t)
	m := withItems(t, "A", "B")
	m = send(m, " ")
	if m.s.Timer != timerGoing {
		t.Fatal("space should start")
	}
	m = runClock(m, 30)
	if m.s.Remaining != 30 {
		t.Fatalf("remaining %d", m.s.Remaining)
	}
	m = send(m, " ")
	m = runClock(m, 10)
	if m.s.Timer != timerPaused || m.s.Remaining != 30 {
		t.Fatalf("pause should freeze: %+v", m.s)
	}
	m = send(m, " ")
	m = runClock(m, 40)
	if m.s.Timer != timerDone {
		t.Fatalf("want done, got %s", m.s.Timer)
	}
	if !strings.Contains(m.View(), "Next: B: B text") {
		t.Fatal("done view should show next item")
	}
	m = send(m, " ")
	if m.s.Current != m.c.Items[1].ID || m.s.Timer != timerIdle || len(m.s.Log) != 1 {
		t.Fatalf("advance failed: %+v", m.s)
	}
	if !m.s.doneThisCycle(m.c.Items[0].ID) {
		t.Fatal("A should be checked this cycle")
	}

	// enter on done advances without a prompt, wrapping the cycle
	m = send(m, " ")
	m = runClock(m, 61)
	m = send(m, "enter")
	if m.prompt != nil || m.s.Current != m.c.Items[0].ID || len(m.s.CycleDone) != 0 {
		t.Fatalf("wrap failed: %+v", m.s)
	}
	if !m.s.Log[1].Last || m.s.Log[0].Last {
		t.Fatal("last-of-cycle flags wrong")
	}
}

func TestSleepPauses(t *testing.T) {
	setup(t)
	m := withItems(t, "A")
	m = send(m, " ")
	m = runClock(m, 5)
	next, _ := m.Update(tickMsg(m.lastTick.Add(10 * time.Minute)))
	m = next.(model)
	if m.s.Timer != timerPaused || m.s.Remaining != 55 {
		t.Fatalf("sleep should pause without burning time: %+v", m.s)
	}
}

func TestPrompts(t *testing.T) {
	setup(t)
	m := withItems(t, "A", "B", "C")

	m = send(m, "enter", "n")
	if len(m.s.Log) != 0 || m.prompt != nil {
		t.Fatal("n should cancel")
	}
	m = send(m, "enter", "y")
	if len(m.s.Log) != 1 || m.s.Log[0].Skipped {
		t.Fatal("complete early failed")
	}
	m = send(m, "s", "y")
	if len(m.s.Log) != 2 || !m.s.Log[1].Skipped {
		t.Fatal("skip failed")
	}
	if !strings.Contains(m.renderCounter(60), "◦") {
		t.Fatal("skip should be hollow in counter")
	}

	m = send(m, "F", "y")
	if m.scr != scrFinish {
		t.Fatal("want finish")
	}
	v := m.View()
	if !strings.Contains(v, "Wednesday, October 21") || !strings.Contains(v, "Finished 1 item (1 skipped)") {
		t.Fatalf("finish view wrong:\n%s", v)
	}
	m = send(m, "x")
	if m.scr != scrCycler || len(m.s.Log) != 0 || m.s.Current != m.c.Items[0].ID {
		t.Fatal("finish should reset")
	}
}

func TestPreviousDayFinish(t *testing.T) {
	setClock := setup(t)
	m := withItems(t, "A", "B")
	m = send(m, "enter", "y")
	m.s.save()

	setClock(testNow.Add(24 * time.Hour))
	m = newModel()
	if m.scr != scrFinish || !strings.Contains(m.View(), "Wednesday, October 21") {
		t.Fatal("previous day should show finish")
	}
	m = send(m, "q")
	if m.scr != scrCycler || len(m.s.Log) != 0 || m.s.Date != "2026-10-22" {
		t.Fatalf("should reset into today: %+v", m.s)
	}

	// nothing done yesterday: silent reset
	setClock(testNow.Add(48 * time.Hour))
	m = newModel()
	if m.scr != scrCycler {
		t.Fatal("empty previous day should not show finish")
	}
}

func TestRolloverHourKeepsDay(t *testing.T) {
	setClock := setup(t)
	m := withItems(t, "A")
	m = send(m, "enter", "y")

	setClock(time.Date(2026, 10, 22, 1, 30, 0, 0, time.Local))
	m = newModel()
	if m.scr != scrCycler || len(m.s.Log) != 1 {
		t.Fatal("1:30am should still be the previous logical day")
	}
	if !strings.Contains(m.dateLabel(), "☾ Thu, Oct 22") {
		t.Fatalf("want moon date, got %q", m.dateLabel())
	}
}

func TestFormGating(t *testing.T) {
	setup(t)
	m := withItems(t, "A")
	m = send(m, "c", "e", "tab")
	m = send(m, "backspace")
	m = typeText(m, "999")
	if v := m.f.mins.Value(); v != "99" {
		t.Fatalf("minutes gate: %q", v)
	}
	m = send(m, "tab", "backspace", "backspace")
	m = typeText(m, "7x5")
	if v := m.f.secs.Value(); v != "7" {
		t.Fatalf("seconds gate: %q", v)
	}

	m = send(m, "esc")
	if m.prompt == nil || m.prompt.kind != confirmDiscard || !m.f.cancelSel {
		t.Fatal("dirty esc should prompt discard")
	}
	m = send(m, "n")
	if m.scr != scrForm {
		t.Fatal("n should stay in form")
	}
	m = send(m, "enter", "y")
	if m.scr != scrConfig || m.c.Items[0].Seconds != 60 {
		t.Fatal("discard should leave item unchanged")
	}

	m = send(m, "e", "esc")
	if m.scr != scrConfig {
		t.Fatal("clean esc should exit immediately")
	}

	m = send(m, "e")
	for range 5 {
		m = send(m, "backspace")
	}
	m = send(m, "tab", "tab", "tab", "tab", "tab", "tab", "tab", "enter")
	if m.f == nil || m.f.err != "Name is required" {
		t.Fatal("empty name should be refused")
	}
}

func TestConfigEdits(t *testing.T) {
	setup(t)
	m := withItems(t, "A", "B", "C")
	a, b, c := m.c.Items[0].ID, m.c.Items[1].ID, m.c.Items[2].ID

	// timer keeps running inside config
	m = send(m, " ", "c")
	m = runClock(m, 10)
	if m.s.Remaining != 50 {
		t.Fatalf("clock should run under config: %d", m.s.Remaining)
	}

	m = send(m, "J")
	if m.c.Items[1].ID != a || m.sel != 1 {
		t.Fatal("J should move item down")
	}
	m = send(m, "esc")
	if m.s.Current != a || m.s.Remaining != 50 {
		t.Fatal("reorder should keep current by id")
	}

	// editing the started current item keeps remaining under a new denominator
	m = send(m, "c", "e", "tab", "backspace")
	m = typeText(m, "2")
	m = send(m, "tab", "tab", "tab", "tab", "tab", "tab", "enter")
	if m.c.byID(a).Seconds != 120 || m.s.Remaining != 50 {
		t.Fatalf("duration edit: %+v", m.s)
	}

	// B was moved above A without running, so it is the next open item
	m = send(m, "d", "y")
	if len(m.c.Items) != 2 || m.s.Current != b || m.s.Timer != timerIdle {
		t.Fatalf("deleting current should hand off to next open: %+v", m.s)
	}
	_ = c
}

func TestLayoutFits(t *testing.T) {
	setup(t)
	sizes := [][2]int{{34, 12}, {50, 16}, {80, 24}, {140, 45}}
	m := withItems(t, "Photobucket", "Joojeh", "Secondary tasks with a long name")
	m.c.Items[0].SourceConfig = strings.Repeat("a very long source value ", 20)
	m.c.Items[1].TimesPerDay = 3
	m.c.Items[2].ActiveDays = []int{int(time.Wednesday), int(time.Friday)}
	m.refreshSources()

	screens := map[string]model{}
	screens["cycler"] = m
	screens["paused"] = send(m, " ", " ")
	screens["prompt"] = send(m, "F")
	screens["config"] = send(m, "c")
	screens["delete"] = send(m, "c", "d")
	screens["form"] = send(m, "c", "e")
	screens["formdays"] = send(m, "c", "e", "tab", "tab", "tab", "tab")
	screens["emptyday"] = withSched(t, item{Name: "A", ActiveDays: []int{}})
	screens["finish"] = send(m, "enter", "y", "F", "y")
	fr := m
	fr.scr = scrFirstRun
	screens["firstrun"] = fr

	for name, s := range screens {
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

func TestReorderKeepsProgress(t *testing.T) {
	setup(t)
	m := withItems(t, "A", "B", "C")
	a, b, c := m.c.Items[0].ID, m.c.Items[1].ID, m.c.Items[2].ID

	// A done, then moved last: still done, C runs next, C closes the cycle
	m = send(m, "enter", "y", "c", "k", "J", "J", "esc")
	if !m.s.doneThisCycle(a) || m.s.Current != b {
		t.Fatalf("progress lost: %+v", m.s)
	}
	m = send(m, "enter", "y")
	if m.s.Current != c {
		t.Fatal("C should run after B")
	}
	m = send(m, "enter", "y")
	if !m.s.Log[2].Last || len(m.s.CycleDone) != 0 {
		t.Fatal("C should close the cycle")
	}

}

func TestReorderOpenAboveCurrent(t *testing.T) {
	setup(t)
	m := withItems(t, "A", "B", "C")
	b, c := m.c.Items[1].ID, m.c.Items[2].ID

	m = send(m, "enter", "y", "c", "j", "K", "esc")
	if m.c.Items[1].ID != c || m.s.Current != b {
		t.Fatalf("setup: %+v", m.s)
	}
	m = send(m, "enter", "y")
	if m.s.Current != c {
		t.Fatal("C moved above B should still run this cycle")
	}
	m = send(m, "enter", "y")
	if !m.s.Log[2].Last {
		t.Fatal("C should close the cycle")
	}
}

func TestSourcesMsg(t *testing.T) {
	setup(t)
	m := withItems(t, "A")
	id := m.c.Items[0].ID
	stale := m.sourcesCmd()
	m.c.Items[0].SourceConfig = "fresh"
	fresh := m.sourcesCmd()

	next, _ := m.Update(fresh())
	m = next.(model)
	next, _ = m.Update(stale())
	m = next.(model)
	if m.src[id].text != "fresh" || m.srcPending != 0 {
		t.Fatalf("stale read applied: %+v pending %d", m.src[id], m.srcPending)
	}
}

func TestLoadConfigRepairsIDs(t *testing.T) {
	setup(t)
	writeJSON(configPath(), &config{Items: []item{
		{Name: "A", Seconds: 60, Source: srcStatic},
		{ID: "x", Name: "B", Seconds: 60, Source: srcStatic},
		{ID: "x", Name: "C", Seconds: 60, Source: srcStatic},
	}})
	c1 := loadConfig()
	c2 := loadConfig()
	ids := map[string]bool{}
	for i, it := range c1.Items {
		if it.ID == "" || ids[it.ID] || c2.Items[i].ID != it.ID {
			t.Fatalf("ids not repaired and persisted: %+v / %+v", c1.Items, c2.Items)
		}
		ids[it.ID] = true
	}
}

// items with explicit scheduling; blank fields get workable defaults
func withSched(t *testing.T, items ...item) model {
	c := &config{}
	for _, it := range items {
		it.ID = newID()
		it.Seconds = 60
		it.Source = srcStatic
		it.SourceConfig = it.Name + " text"
		if it.ActiveDays == nil {
			it.ActiveDays = allDays()
		}
		c.Items = append(c.Items, it)
	}
	c.save()
	return newModel()
}

func TestLegacyConfigGetsEveryDay(t *testing.T) {
	setup(t)
	writeJSON(configPath(), &config{Items: []item{
		{ID: "a", Name: "A", Seconds: 60, Source: srcStatic},
	}})
	if got := loadConfig().Items[0].daysLabel(); got != "Every day" {
		t.Fatalf("missing days should mean every day, got %q", got)
	}
	// the repair is written back, so the next load sees it explicitly
	if got := loadConfig().Items[0].ActiveDays; len(got) != 7 {
		t.Fatalf("repair not persisted: %v", got)
	}

	writeJSON(configPath(), &config{Items: []item{
		{ID: "a", Name: "A", Seconds: 60, Source: srcStatic, ActiveDays: []int{}},
	}})
	if got := loadConfig().Items[0].daysLabel(); got != "Never" {
		t.Fatalf("an explicit empty list means never, got %q", got)
	}
}

func TestDaysLabel(t *testing.T) {
	cases := []struct {
		days []int
		want string
	}{
		{allDays(), "Every day"},
		{weekdayDays(), "Weekdays"},
		{[]int{}, "Never"},
		{[]int{int(time.Saturday), int(time.Monday)}, "Mo Sa"},
		{normalizeDays([]int{5, 0, 1, 2, 3}), "Mo Tu We Fr Su"},
	}
	for _, c := range cases {
		if got := (item{ActiveDays: c.days}).daysLabel(); got != c.want {
			t.Errorf("%v: want %q, got %q", c.days, c.want, got)
		}
	}
}

func TestDailyLimitExhausts(t *testing.T) {
	setup(t)
	m := withSched(t, item{Name: "A", TimesPerDay: 1}, item{Name: "B"})
	a, b := m.c.Items[0].ID, m.c.Items[1].ID

	m = send(m, "enter", "y")
	if m.s.Current != b {
		t.Fatal("B should run after A")
	}
	m = send(m, "enter", "y")
	if m.s.Current != b {
		t.Fatalf("A is used up, so B starts the new cycle: %+v", m.s)
	}
	if m.s.eligible(*m.c.byID(a)) {
		t.Fatal("A should be out for the day")
	}
	if cs, _ := m.crumbs(); len(cs) != 1 {
		t.Fatalf("exhausted A should leave the trail: %v", cs)
	}

	// raising the limit puts it straight back in the cycle
	m.c.Items[0].TimesPerDay = 2
	if !m.s.eligible(*m.c.byID(a)) {
		t.Fatal("a raised limit should re-admit the item")
	}
}

func TestSkipCountsAgainstLimit(t *testing.T) {
	setup(t)
	m := withSched(t, item{Name: "A", TimesPerDay: 1}, item{Name: "B"})
	a := m.c.Items[0].ID

	m = send(m, "s", "y")
	if m.s.countToday(a) != 1 || m.s.eligible(*m.c.byID(a)) {
		t.Fatalf("a skip should spend a repeat: %+v", m.s)
	}
}

func TestWeekdayGatingUsesLogicalDay(t *testing.T) {
	setClock := setup(t)
	// 1:30am Thursday still belongs to Wednesday
	setClock(time.Date(2026, 10, 22, 1, 30, 0, 0, time.Local))
	m := withSched(t,
		item{Name: "Thu", ActiveDays: []int{int(time.Thursday)}},
		item{Name: "Wed", ActiveDays: []int{int(time.Wednesday)}},
	)
	if it := m.c.byID(m.s.Current); it == nil || it.Name != "Wed" {
		t.Fatalf("want Wednesday's item, got %+v", it)
	}
}

func TestNeverItemNeverRuns(t *testing.T) {
	setup(t)
	m := withSched(t, item{Name: "A", ActiveDays: []int{}}, item{Name: "B"})
	b := m.c.Items[1].ID
	if m.s.Current != b {
		t.Fatal("a Never item should not become current")
	}
	m = send(m, "enter", "y")
	if m.s.Current != b {
		t.Fatalf("the cycle should be B alone: %+v", m.s)
	}
	if cs, _ := m.crumbs(); len(cs) != 1 {
		t.Fatalf("Never item should stay off the trail: %v", cs)
	}
}

func TestEmptyDay(t *testing.T) {
	setup(t)
	m := withSched(t, item{Name: "A", ActiveDays: []int{}})
	if m.s.Current != "" || m.scr != scrCycler {
		t.Fatalf("nothing to run should leave no current item: %+v", m.s)
	}
	if !strings.Contains(m.View(), "Enjoy your day off") {
		t.Fatalf("want the empty-day copy:\n%s", m.View())
	}
	// the timer keys are dead, the day keys are not
	m = send(m, " ", "enter", "s", "p")
	if m.s.Timer != timerIdle || m.prompt != nil || len(m.s.Log) != 0 {
		t.Fatalf("timer keys should be inert: %+v", m.s)
	}
	m = send(m, "F", "y")
	if m.scr != scrFinish {
		t.Fatal("finish day should still work")
	}
}

func TestPassReturnsNextCycle(t *testing.T) {
	setup(t)
	m := withSched(t, item{Name: "A", TimesPerDay: 2}, item{Name: "B"})
	a, b := m.c.Items[0].ID, m.c.Items[1].ID

	m = send(m, "p", "y")
	if m.s.Current != b || len(m.s.Log) != 0 || !m.s.passedThisCycle(a) {
		t.Fatalf("pass should step over A silently: %+v", m.s)
	}
	if m.s.countToday(a) != 0 {
		t.Fatal("a pass should not spend a repeat")
	}

	m = send(m, "enter", "y")
	if m.s.Current != a || len(m.s.Passed) != 0 {
		t.Fatalf("A should come back next cycle: %+v", m.s)
	}
	if !m.s.Log[0].Last {
		t.Fatal("B closed the cycle")
	}

	// B has no limit, so it cannot be passed
	m = send(m, "enter", "y")
	if m.s.Current != b {
		t.Fatal("setup: B should be current")
	}
	m = send(m, "p")
	if m.prompt != nil {
		t.Fatal("pass should be inert on an unrationed item")
	}
}

func TestPassOnSoleItemResets(t *testing.T) {
	setup(t)
	m := withSched(t, item{Name: "A", TimesPerDay: 1})
	a := m.c.Items[0].ID
	m = send(m, " ")
	m = runClock(m, 10)

	m = send(m, "p", "y")
	if m.s.Current != a || m.s.Timer != timerIdle || m.s.Remaining != 60 {
		t.Fatalf("passing the only item should reset its timer: %+v", m.s)
	}
	if len(m.s.Log) != 0 || len(m.s.Passed) != 0 {
		t.Fatalf("nothing should be recorded: %+v", m.s)
	}
}

func TestPassClosingCycleMarksLast(t *testing.T) {
	setup(t)
	m := withSched(t, item{Name: "A"}, item{Name: "B", TimesPerDay: 1})
	a := m.c.Items[0].ID

	m = send(m, "enter", "y")
	m = send(m, "p", "y")
	if m.s.Current != a {
		t.Fatalf("the cycle should restart on A: %+v", m.s)
	}
	if len(m.s.Log) != 1 || !m.s.Log[0].Last {
		t.Fatalf("A was the last thing logged in that cycle: %+v", m.s.Log)
	}
}

func TestEditToNeverHandsOff(t *testing.T) {
	setup(t)
	m := withSched(t, item{Name: "A"}, item{Name: "B"})
	b := m.c.Items[1].ID

	// open A, walk to the day cells, switch all seven off, save
	m = send(m, "c", "e", "tab", "tab", "tab", "tab", "right", "right")
	for range 7 {
		m = send(m, " ", "right")
	}
	m = send(m, "tab", "tab", "tab", "enter")

	if m.scr != scrConfig || m.c.Items[0].daysLabel() != "Never" {
		t.Fatalf("days not saved: %+v", m.c.Items[0])
	}
	if m.s.Current != b {
		t.Fatalf("scheduling the current item out should hand off: %+v", m.s)
	}
}

func TestFormPresetsAndLimit(t *testing.T) {
	setup(t)
	m := withSched(t, item{Name: "A"})
	m = send(m, "c", "e", "tab", "tab", "tab")
	m = typeText(m, "0")
	if v := m.f.times.Value(); v != "0" {
		t.Fatalf("times input: %q", v)
	}
	m = typeText(m, "9x9")
	if v := m.f.times.Value(); v != "09" {
		t.Fatalf("times should gate to two digits: %q", v)
	}

	m = send(m, "tab", " ")
	if m.f.dayCur != cellEveryDay || !slices.Equal(m.f.days, allDays()) {
		t.Fatalf("every day preset: %v", m.f.days)
	}
	m = send(m, "right", " ")
	if !slices.Equal(m.f.days, weekdayDays()) {
		t.Fatalf("weekdays preset: %v", m.f.days)
	}
	// toggling one weekend day back on leaves a custom set
	m = send(m, "right", "right", "right", "right", "right", "right", " ")
	if lbl := (item{ActiveDays: m.f.days}).daysLabel(); lbl != "Mo Tu We Th Fr Sa" {
		t.Fatalf("custom set: %q", lbl)
	}

	m = send(m, "tab", "tab", "tab", "enter")
	if it := m.c.Items[0]; it.TimesPerDay != 9 || it.daysLabel() != "Mo Tu We Th Fr Sa" {
		t.Fatalf("saved item: %+v", it)
	}
	if m.c.Items[0].limitLabel() != "9" {
		t.Fatal("limit label")
	}
}

func TestZeroLimitSavesAsNoLimit(t *testing.T) {
	setup(t)
	m := withSched(t, item{Name: "A", TimesPerDay: 3})
	m = send(m, "c", "e", "tab", "tab", "tab", "backspace")
	m = typeText(m, "0")
	m = send(m, "tab", "tab", "tab", "tab", "enter")
	if it := m.c.Items[0]; it.TimesPerDay != 0 || it.limitLabel() != "no limit" {
		t.Fatalf("0 should clear the limit: %+v", it)
	}
}
