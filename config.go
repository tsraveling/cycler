package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// @region app:config -- CYCLER ITEMS + PREFS

const (
	maxNameLen     = 24
	maxMinutes     = 180
	maxTimesPerDay = 99
)

type item struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Seconds int    `json:"seconds"`
	// repeats allowed per day; 0 is no limit
	TimesPerDay int `json:"timesPerDay"`
	// time.Weekday values the item runs on; empty is never
	ActiveDays   []int      `json:"activeDays"`
	Source       sourceType `json:"source"`
	SourceConfig string     `json:"sourceConfig"`
}

type config struct {
	Items    []item `json:"items"`
	HelpFull bool   `json:"helpFull"`
}

// the order days are stored and shown in
var weekOrder = []time.Weekday{
	time.Monday, time.Tuesday, time.Wednesday, time.Thursday,
	time.Friday, time.Saturday, time.Sunday,
}

var dayAbbrev = map[time.Weekday]string{
	time.Monday:    "Mo",
	time.Tuesday:   "Tu",
	time.Wednesday: "We",
	time.Thursday:  "Th",
	time.Friday:    "Fr",
	time.Saturday:  "Sa",
	time.Sunday:    "Su",
}

func allDays() []int {
	out := make([]int, 0, len(weekOrder))
	for _, d := range weekOrder {
		out = append(out, int(d))
	}
	return out
}

func weekdayDays() []int {
	out := make([]int, 0, 5)
	for _, d := range weekOrder[:5] {
		out = append(out, int(d))
	}
	return out
}

// deduped, week order, out-of-range dropped. never nil, so an empty result
// reads as a deliberate "never" rather than a missing field.
func normalizeDays(days []int) []int {
	seen := map[int]bool{}
	for _, d := range days {
		if d >= 0 && d <= 6 {
			seen[d] = true
		}
	}
	out := []int{}
	for _, d := range weekOrder {
		if seen[int(d)] {
			out = append(out, int(d))
		}
	}
	return out
}

func (it item) activeOn(wd time.Weekday) bool { return slices.Contains(it.ActiveDays, int(wd)) }

func (it item) daysLabel() string {
	var on []time.Weekday
	for _, d := range weekOrder {
		if it.activeOn(d) {
			on = append(on, d)
		}
	}
	switch {
	case len(on) == 0:
		return "Never"
	case len(on) == 7:
		return "Every day"
	case len(on) == 5 && !it.activeOn(time.Saturday) && !it.activeOn(time.Sunday):
		return "Weekdays"
	}
	parts := make([]string, len(on))
	for i, d := range on {
		parts[i] = dayAbbrev[d]
	}
	return strings.Join(parts, " ")
}

func (it item) limitLabel() string {
	if it.TimesPerDay < 1 {
		return "no limit"
	}
	return strconv.Itoa(it.TimesPerDay)
}

// overridable so tests never touch the real home directory
var configDirFn = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	return filepath.Join(home, ".config", "cycler")
}

func configPath() string { return filepath.Join(configDirFn(), "config.json") }

// a missing or corrupt file yields an empty config rather than an error
func loadConfig() *config {
	c := &config{}
	data, err := os.ReadFile(configPath())
	if err != nil {
		return c
	}
	if json.Unmarshal(data, c) != nil {
		return &config{}
	}
	seen := map[string]bool{}
	repaired := false
	valid := c.Items[:0]
	for _, it := range c.Items {
		if it.ID == "" || seen[it.ID] {
			it.ID = newID()
			repaired = true
		}
		seen[it.ID] = true
		if it.Seconds < 1 {
			it.Seconds = 1
		}
		if !it.Source.valid() {
			it.Source = srcStatic
		}
		if it.TimesPerDay < 0 {
			it.TimesPerDay = 0
			repaired = true
		}
		if it.TimesPerDay > maxTimesPerDay {
			it.TimesPerDay = maxTimesPerDay
			repaired = true
		}
		// a day list that is missing, or that nothing survives, means every day.
		// only a list deliberately left empty means never.
		days := normalizeDays(it.ActiveDays)
		if it.ActiveDays == nil || (len(days) == 0 && len(it.ActiveDays) != 0) {
			days = allDays()
		}
		if !slices.Equal(days, it.ActiveDays) {
			it.ActiveDays = days
			repaired = true
		}
		valid = append(valid, it)
	}
	c.Items = valid
	if repaired {
		c.save()
	}
	return c
}

func (c *config) save() {
	writeJSON(configPath(), c)
}

func (c *config) indexOf(id string) int {
	for i, it := range c.Items {
		if it.ID == id {
			return i
		}
	}
	return -1
}

func (c *config) byID(id string) *item {
	if i := c.indexOf(id); i >= 0 {
		return &c.Items[i]
	}
	return nil
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// best-effort atomic write: temp file, then rename over the target. a
// symlinked target is written through to the file it points at.
func writeJSON(path string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	dir := filepath.Dir(path)
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	serr := tmp.Sync()
	cerr := tmp.Close()
	os.Chmod(tmp.Name(), mode)
	if werr != nil || serr != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		os.Remove(tmp.Name())
	}
}
