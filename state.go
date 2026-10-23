package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// @region app:state -- TODAY'S CYCLE

// indirection so tests can pin the clock
var nowFn = time.Now

const (
	rolloverHour = 4
	dateLayout   = "2006-01-02"
)

type timerState string

const (
	timerIdle   timerState = "idle"
	timerGoing  timerState = "going"
	timerPaused timerState = "paused"
	timerDone   timerState = "done"
)

// one advanced item. Name is snapshotted so later config edits can't rewrite
// the day's history.
type entry struct {
	ItemID  string `json:"itemId"`
	Name    string `json:"name"`
	Skipped bool   `json:"skipped"`
	// was the last item of its cycle
	Last bool `json:"last"`
}

type dayState struct {
	// logical date, YYYY-MM-DD, shifted by rolloverHour
	Date      string     `json:"date"`
	Current   string     `json:"current"`
	Timer     timerState `json:"timer"`
	Remaining int        `json:"remaining"`
	Log       []entry    `json:"log"`
	// item IDs advanced in the current cycle
	CycleDone []string `json:"cycleDone"`
	// item IDs stepped over for this cycle only, unlogged
	Passed []string `json:"passed"`
}

func statePath() string { return filepath.Join(configDirFn(), "state.json") }

// the moment shifted into the day it belongs to. hours before rolloverHour
// belong to the previous calendar day, so working past midnight doesn't end
// the day.
func logicalTime(t time.Time) time.Time {
	if t.Hour() < rolloverHour {
		t = t.AddDate(0, 0, -1)
	}
	return t
}

func logicalDate(t time.Time) string { return logicalTime(t).Format(dateLayout) }

// nil when there is no usable state file
func loadState() *dayState {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return nil
	}
	var s dayState
	if json.Unmarshal(data, &s) != nil || s.Date == "" {
		return nil
	}
	// a timer that was running when we quit resumes paused
	switch s.Timer {
	case timerIdle, timerPaused, timerDone:
	default:
		s.Timer = timerPaused
	}
	s.Remaining = max(0, s.Remaining)
	return &s
}

func (s *dayState) save() { writeJSON(statePath(), s) }

func freshState(date string, c *config) *dayState {
	s := &dayState{Date: date, Timer: timerIdle}
	s.setCurrent(c, s.firstEligible(c))
	return s
}

func (s *dayState) counts() (done, skipped int) {
	for _, e := range s.Log {
		if e.Skipped {
			skipped++
		} else {
			done++
		}
	}
	return
}

// the weekday the day's items are scheduled against
func (s *dayState) weekday() time.Weekday {
	t, err := time.ParseInLocation(dateLayout, s.Date, time.Local)
	if err != nil {
		return logicalTime(nowFn()).Weekday()
	}
	return t.Weekday()
}

// times the item has been advanced today, skips included
func (s *dayState) countToday(id string) int {
	n := 0
	for _, e := range s.Log {
		if e.ItemID == id {
			n++
		}
	}
	return n
}

// runs on today's weekday and has repeats left
func (s *dayState) eligible(it item) bool {
	if !it.activeOn(s.weekday()) {
		return false
	}
	return it.TimesPerDay < 1 || s.countToday(it.ID) < it.TimesPerDay
}

func (s *dayState) doneThisCycle(id string) bool {
	return slices.Contains(s.CycleDone, id)
}

func (s *dayState) passedThisCycle(id string) bool {
	return slices.Contains(s.Passed, id)
}

// first item that can run today, in list order; -1 when the day is empty
func (s *dayState) firstEligible(c *config) int {
	for i, it := range c.Items {
		if s.eligible(it) {
			return i
		}
	}
	return -1
}

// points the cycle at item i with a fresh, unstarted timer. a negative or
// out-of-range index leaves no current item, which is the empty-day state.
func (s *dayState) setCurrent(c *config, i int) {
	if i < 0 || i >= len(c.Items) {
		s.Current = ""
		s.Timer = timerIdle
		s.Remaining = 0
		return
	}
	s.Current = c.Items[i].ID
	s.Timer = timerIdle
	s.Remaining = c.Items[i].Seconds
}

// keeps Current pointing at an item that can still run today
func (s *dayState) reconcile(c *config) {
	s.CycleDone = slices.DeleteFunc(s.CycleDone, func(id string) bool { return c.byID(id) == nil })
	s.Passed = slices.DeleteFunc(s.Passed, func(id string) bool { return c.byID(id) == nil })
	it := c.byID(s.Current)
	if it == nil || !s.eligible(*it) {
		s.handOff(c)
		return
	}
	if s.Timer == timerIdle {
		s.Remaining = it.Seconds
	}
	s.Remaining = clamp(s.Remaining, 0, it.Seconds)
}

// ends the cycle: the newest entry logged in it becomes its last, and the
// per-cycle sets reset
func (s *dayState) closeCycle() {
	if n := len(s.Log); n > 0 && s.doneThisCycle(s.Log[n-1].ItemID) {
		s.Log[n-1].Last = true
	}
	s.CycleDone = nil
	s.Passed = nil
}

// logs the current item and moves to the next, wrapping into a new cycle
// after the last one
func (s *dayState) advance(c *config, skipped bool) {
	i := c.indexOf(s.Current)
	if i < 0 {
		s.handOff(c)
		return
	}
	next := s.nextOpen(c)
	s.Log = append(s.Log, entry{ItemID: s.Current, Name: c.Items[i].Name, Skipped: skipped, Last: next < 0})
	s.CycleDone = append(s.CycleDone, s.Current)
	if next < 0 {
		s.CycleDone = nil
		s.Passed = nil
		next = s.firstEligible(c)
	}
	s.setCurrent(c, next)
}

// steps over the current item for this cycle only: no log entry, no counter
// dot, no cost against its daily limit
func (s *dayState) pass(c *config) {
	if c.indexOf(s.Current) < 0 {
		s.handOff(c)
		return
	}
	s.Passed = append(s.Passed, s.Current)
	next := s.nextOpen(c)
	if next < 0 {
		s.closeCycle()
		next = s.firstEligible(c)
	}
	s.setCurrent(c, next)
}

// moves on from a current item that left the cycle without being logged. when
// it was the last open one the cycle closes on the newest entry of this cycle.
func (s *dayState) handOff(c *config) {
	next := s.nextOpen(c)
	if next < 0 {
		s.closeCycle()
		next = s.firstEligible(c)
	}
	s.setCurrent(c, next)
}

// first item in list order that can still run this cycle, other than the
// current one; -1 when the current item closes the cycle
func (s *dayState) nextOpen(c *config) int {
	for i, it := range c.Items {
		if it.ID == s.Current || s.doneThisCycle(it.ID) || s.passedThisCycle(it.ID) {
			continue
		}
		if s.eligible(it) {
			return i
		}
	}
	return -1
}

// the item that runs after the current one, wrapping into a new cycle; -1 when
// nothing is left to run today. assumes the current item is about to be
// logged, so its run in progress is discounted: on its last allowed repeat it
// cannot be its own successor.
func (s *dayState) upcoming(c *config) int {
	if next := s.nextOpen(c); next >= 0 {
		return next
	}
	for i, it := range c.Items {
		if it.ID == s.Current && it.TimesPerDay > 0 && s.countToday(it.ID)+1 >= it.TimesPerDay {
			continue
		}
		if s.eligible(it) {
			return i
		}
	}
	return -1
}
