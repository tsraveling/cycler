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
}

func statePath() string { return filepath.Join(configDirFn(), "state.json") }

// the day a moment belongs to. hours before rolloverHour belong to the
// previous calendar day, so working past midnight doesn't end the day.
func logicalDate(t time.Time) string {
	if t.Hour() < rolloverHour {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format(dateLayout)
}

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
	if len(c.Items) > 0 {
		s.Current = c.Items[0].ID
		s.Remaining = c.Items[0].Seconds
	}
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

func (s *dayState) doneThisCycle(id string) bool {
	return slices.Contains(s.CycleDone, id)
}

// points the cycle at item i with a fresh, unstarted timer
func (s *dayState) setCurrent(c *config, i int) {
	if len(c.Items) == 0 {
		s.Current = ""
		s.Timer = timerIdle
		s.Remaining = 0
		return
	}
	i = clamp(i, 0, len(c.Items)-1)
	s.Current = c.Items[i].ID
	s.Timer = timerIdle
	s.Remaining = c.Items[i].Seconds
}

// keeps Current pointing at a real item; a dangling one restarts the list
func (s *dayState) reconcile(c *config) {
	if len(c.Items) == 0 {
		s.setCurrent(c, 0)
		return
	}
	it := c.byID(s.Current)
	if it == nil {
		s.CycleDone = nil
		s.setCurrent(c, 0)
		return
	}
	if s.Timer == timerIdle {
		s.Remaining = it.Seconds
	}
	s.Remaining = clamp(s.Remaining, 0, it.Seconds)
	s.CycleDone = slices.DeleteFunc(s.CycleDone, func(id string) bool { return c.byID(id) == nil })
}

// logs the current item and moves to the next, wrapping into a new cycle
// after the last one
func (s *dayState) advance(c *config, skipped bool) {
	i := c.indexOf(s.Current)
	if i < 0 {
		s.CycleDone = nil
		s.setCurrent(c, 0)
		return
	}
	next := s.nextOpen(c)
	s.Log = append(s.Log, entry{ItemID: s.Current, Name: c.Items[i].Name, Skipped: skipped, Last: next < 0})
	if next < 0 {
		s.CycleDone = nil
		s.setCurrent(c, 0)
		return
	}
	s.CycleDone = append(s.CycleDone, s.Current)
	s.setCurrent(c, next)
}

// moves on from a current item that left the list without being logged. when
// it was the last open one the cycle closes on the newest entry of this cycle.
func (s *dayState) handOff(c *config) {
	next := s.nextOpen(c)
	if next < 0 {
		if n := len(s.Log); n > 0 && s.doneThisCycle(s.Log[n-1].ItemID) {
			s.Log[n-1].Last = true
		}
		s.CycleDone = nil
	}
	s.setCurrent(c, max(0, next))
}

// first item in list order not yet done this cycle, other than the current
// one; -1 when the current item closes the cycle
func (s *dayState) nextOpen(c *config) int {
	for i, it := range c.Items {
		if it.ID != s.Current && !s.doneThisCycle(it.ID) {
			return i
		}
	}
	return -1
}

// the item that runs after the current one, wrapping into a new cycle
func (s *dayState) upcoming(c *config) int {
	return max(0, s.nextOpen(c))
}
