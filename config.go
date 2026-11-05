package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// @region app:config -- CYCLER ITEMS + PREFS

const (
	maxNameLen     = 24
	maxTitleLen    = 24
	maxMinutes     = 180
	maxTimesPerDay = 99
	minsPerDay     = 24 * 60
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

type blockType string

const (
	blkStandard blockType = "standard"
	blkCycler   blockType = "cycler"
)

var blockTypes = []blockType{blkStandard, blkCycler}

func (t blockType) valid() bool { return slices.Contains(blockTypes, t) }

func (t blockType) label() string {
	if t == blkCycler {
		return "Cycler"
	}
	return "Standard"
}

// a titled window of wall-clock time. Start and End are minutes from midnight,
// half-open [Start, End), and never cross midnight.
type block struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	Type  blockType `json:"type"`
	// key into blockColors; ignored for a cycler block
	Color        string     `json:"color"`
	Start        int        `json:"start"`
	End          int        `json:"end"`
	ActiveDays   []int      `json:"activeDays"`
	Source       sourceType `json:"source"`
	SourceConfig string     `json:"sourceConfig"`
}

type config struct {
	Items    []item  `json:"items"`
	Blocks   []block `json:"blocks"`
	HelpFull bool    `json:"helpFull"`
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

func activeOn(days []int, wd time.Weekday) bool { return slices.Contains(days, int(wd)) }

func (it item) activeOn(wd time.Weekday) bool { return activeOn(it.ActiveDays, wd) }
func (b block) activeOn(wd time.Weekday) bool { return activeOn(b.ActiveDays, wd) }
func (it item) daysLabel() string             { return daysLabel(it.ActiveDays) }
func (b block) daysLabel() string             { return daysLabel(b.ActiveDays) }

func daysLabel(days []int) string {
	var on []time.Weekday
	for _, d := range weekOrder {
		if activeOn(days, d) {
			on = append(on, d)
		}
	}
	switch {
	case len(on) == 0:
		return "Never"
	case len(on) == 7:
		return "Every day"
	case len(on) == 5 && !activeOn(days, time.Saturday) && !activeOn(days, time.Sunday):
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

	blocks := c.Blocks[:0]
	for _, b := range c.Blocks {
		if b.ID == "" || seen[b.ID] {
			b.ID = newID()
			repaired = true
		}
		seen[b.ID] = true
		if !b.Type.valid() {
			b.Type = blkStandard
			repaired = true
		}
		if blockColors[blockColorIndex(b.Color)].key != b.Color {
			b.Color = defaultBlockColor
			repaired = true
		}
		if s, e := clamp(b.Start, 0, minsPerDay-1), clamp(b.End, 0, minsPerDay-1); s != b.Start || e != b.End {
			b.Start, b.End = s, e
			repaired = true
		}
		if !b.Source.valid() {
			b.Source = srcStatic
			repaired = true
		}
		days := normalizeDays(b.ActiveDays)
		if b.ActiveDays == nil || (len(days) == 0 && len(b.ActiveDays) != 0) {
			days = allDays()
		}
		if !slices.Equal(days, b.ActiveDays) {
			b.ActiveDays = days
			repaired = true
		}
		// a window that never opens is unrecoverable, so it goes
		if b.End <= b.Start {
			repaired = true
			continue
		}
		blocks = append(blocks, b)
	}
	before := make([]string, len(blocks))
	for i, b := range blocks {
		before[i] = b.ID
	}
	c.Blocks = blocks
	c.sortBlocks()
	for i, b := range c.Blocks {
		if b.ID != before[i] {
			repaired = true
			break
		}
	}

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

func (c *config) blockIndexOf(id string) int {
	for i, b := range c.Blocks {
		if b.ID == id {
			return i
		}
	}
	return -1
}

func (c *config) blockByID(id string) *block {
	if i := c.blockIndexOf(id); i >= 0 {
		return &c.Blocks[i]
	}
	return nil
}

// stored order is display order, so the list never has to sort to render
func (c *config) sortBlocks() {
	slices.SortStableFunc(c.Blocks, func(a, b block) int {
		if a.Start != b.Start {
			return a.Start - b.Start
		}
		return strings.Compare(a.Title, b.Title)
	})
}

// first other block sharing a weekday with b whose window intersects it
func (c *config) overlaps(b block) *block {
	for i, o := range c.Blocks {
		if o.ID == b.ID || b.Start >= o.End || o.Start >= b.End {
			continue
		}
		for _, d := range b.ActiveDays {
			if slices.Contains(o.ActiveDays, d) {
				return &c.Blocks[i]
			}
		}
	}
	return nil
}

// a cycler block has no color of its own; it borrows the cycler's idle orange
func (b block) colorOf() lipgloss.Color {
	if b.Type == blkCycler {
		return orange
	}
	return blockColor(b.Color)
}

// minutes from midnight as 15:04
func fmtClock(min int) string {
	return fmt.Sprintf("%02d:%02d", (min/60)%24, min%60)
}

// minutes from midnight as 1504, for breadcrumbs
func fmtClockCompact(min int) string {
	return fmt.Sprintf("%02d%02d", (min/60)%24, min%60)
}

// both cells filled, numeric and in range
func parseClock(hh, mm string) (int, bool) {
	h, herr := strconv.Atoi(hh)
	m, merr := strconv.Atoi(mm)
	if herr != nil || merr != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
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
