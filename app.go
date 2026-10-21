package main

import (
	"fmt"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// @region app:model -- ROOT MODEL, CLOCK + SCREEN ROUTING

type screen int

const (
	scrFirstRun screen = iota
	scrCycler
	scrConfig
	scrForm
	scrFinish
)

const (
	tickEvery = 200 * time.Millisecond
	// a gap this long between ticks means the machine slept
	sleepGap  = 5 * time.Second
	saveEvery = 10 * time.Second
	pollEvery = 3 * time.Second
)

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type finishInfo struct {
	date          time.Time
	done, skipped int
}

type model struct {
	scr screen
	c   *config
	s   *dayState
	src map[string]sourceResult

	w, h   int
	prompt *confirm

	lastTick time.Time
	lastSave time.Time
	lastPoll time.Time
	carry    float64
	// paused-state blink phase, flipped every tick
	blink bool

	// newest source read issued, and reads still running
	srcGen     int
	srcPending int
	srcIssued  time.Time

	// config list selection
	sel int

	f *form

	fin finishInfo

	quitting bool
}

func newModel() model {
	now := nowFn()
	today := logicalDate(now)
	m := model{c: loadConfig(), w: 80, h: 24, lastTick: now, lastSave: now, lastPoll: now}

	s := loadState()
	switch {
	case s == nil:
		s = freshState(today, m.c)
	case s.Date != today:
		if len(s.Log) > 0 {
			m.fin = finishFor(s)
			m.scr = scrFinish
		}
		s = freshState(today, m.c)
	}
	s.reconcile(m.c)
	m.s = s
	m.s.save()
	m.refreshSources()

	if m.scr != scrFinish {
		m.scr = m.homeScreen()
	}
	return m
}

func finishFor(s *dayState) finishInfo {
	d, err := time.ParseInLocation(dateLayout, s.Date, time.Local)
	if err != nil {
		d = nowFn()
	}
	done, skipped := s.counts()
	return finishInfo{date: d, done: done, skipped: skipped}
}

// where to land when no other screen claims the session
func (m model) homeScreen() screen {
	if len(m.c.Items) == 0 {
		return scrFirstRun
	}
	return scrCycler
}

func (m model) Init() tea.Cmd { return tickCmd() }

type sourcesMsg struct {
	gen     int
	results map[string]sourceResult
	// the open form's preview and the input it was read for
	preview *sourceResult
	from    previewKey
}

// a read still running after this long no longer holds back the poll, up to
// maxSrcPending stuck reads
const (
	srcStaleAfter = 10 * time.Second
	maxSrcPending = 3
)

func readSources(items []item) map[string]sourceResult {
	out := make(map[string]sourceResult, len(items))
	for _, it := range items {
		out[it.ID] = readSource(it.Source, it.SourceConfig)
	}
	return out
}

// reads every source inline; only for startup
func (m *model) refreshSources() {
	m.src = readSources(m.c.Items)
}

// reads every source off the update loop. only the newest read is applied.
func (m *model) sourcesCmd() tea.Cmd {
	m.srcGen++
	m.srcPending++
	m.srcIssued = nowFn()
	gen := m.srcGen
	items := slices.Clone(m.c.Items)
	var key *previewKey
	if m.f != nil {
		k := m.f.key()
		key = &k
	}
	return func() tea.Msg {
		msg := sourcesMsg{gen: gen, results: readSources(items)}
		if key != nil {
			r := readSource(key.typ, key.cfg)
			msg.preview, msg.from = &r, *key
		}
		return msg
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		if m.f != nil {
			m.f.resize(m.w)
		}
		return m, nil

	case tickMsg:
		cmd := m.tick(time.Time(msg))
		return m, tea.Batch(tickCmd(), cmd)

	case sourcesMsg:
		m.srcPending = max(0, m.srcPending-1)
		if msg.gen != m.srcGen {
			return m, nil
		}
		m.src = msg.results
		if m.f != nil && msg.preview != nil && m.f.key() == msg.from {
			m.f.preview = *msg.preview
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		switch m.scr {
		case scrFirstRun:
			return m.updateFirstRun(msg)
		case scrCycler:
			return m.updateCycler(msg)
		case scrConfig:
			return m.updateConfig(msg)
		case scrForm:
			return m.updateForm(msg)
		case scrFinish:
			m.scr = m.homeScreen()
			return m, nil
		}
	}

	if m.f != nil {
		return m, m.f.passThrough(msg)
	}
	return m, nil
}

// advances the clock by real elapsed time, under every screen
func (m *model) tick(now time.Time) tea.Cmd {
	delta := max(0, now.Sub(m.lastTick))
	m.lastTick = now
	m.blink = !m.blink

	if m.s.Timer == timerGoing {
		if delta > sleepGap {
			m.s.Timer = timerPaused
			m.carry = 0
			m.s.save()
		} else {
			m.carry += delta.Seconds()
			whole := int(m.carry)
			m.carry -= float64(whole)
			m.s.Remaining -= whole
			if m.s.Remaining <= 0 {
				m.s.Remaining = 0
				m.s.Timer = timerDone
				if it := m.c.byID(m.s.Current); it != nil {
					notifyFn(it.Name)
				}
				m.s.save()
			} else if now.Sub(m.lastSave) >= saveEvery {
				m.lastSave = now
				m.s.save()
			}
		}
	}

	if now.Sub(m.lastPoll) >= pollEvery {
		m.lastPoll = now
		m.checkRollover(now)
		stale := now.Sub(m.srcIssued) > srcStaleAfter && m.srcPending < maxSrcPending
		if m.srcPending == 0 || stale {
			return m.sourcesCmd()
		}
	}
	return nil
}

// a new logical day arriving mid-session ends the old one, but never under a
// running timer or an open prompt
func (m *model) checkRollover(now time.Time) {
	if m.scr != scrCycler || m.prompt != nil || m.s.Timer == timerGoing {
		return
	}
	today := logicalDate(now)
	if m.s.Date == today {
		return
	}
	if len(m.s.Log) > 0 {
		m.fin = finishFor(m.s)
		m.scr = scrFinish
	}
	m.s = freshState(today, m.c)
	m.carry = 0
	m.s.save()
}

// a running timer freezes on the way out, so its remaining time is kept
func (m model) quit() (tea.Model, tea.Cmd) {
	if m.s.Timer == timerGoing {
		m.s.Timer = timerPaused
	}
	m.s.save()
	m.quitting = true
	return m, tea.Quit
}

func (m *model) toggleHelp() {
	m.c.HelpFull = !m.c.HelpFull
	m.c.save()
}

// @region app:screens -- FIRST RUN + FINISH

func (m model) updateFirstRun(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.quit()
	case "c", "C":
		cmd := m.openConfig()
		return m, cmd
	}
	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	if tooSmall(m.w, m.h) {
		return tooSmallView(m.w, m.h)
	}
	switch m.scr {
	case scrFirstRun:
		return m.firstRunView()
	case scrConfig:
		return m.configView()
	case scrForm:
		return m.formView()
	case scrFinish:
		return m.finishView()
	}
	return m.cyclerView()
}

func (m model) firstRunView() string {
	cw := contentWidth(m.w)
	var body []string
	for _, l := range wrapText("Press C to set up your Cycler.", cw) {
		body = append(body, center(textStyle.Render(l), cw))
	}
	return centeredPanel(body, fixedHelp(firstRunHelp, cw), m.w, m.h, gray)
}

func (m model) finishView() string {
	cw := contentWidth(m.w)
	noun := "items"
	if m.fin.done == 1 {
		noun = "item"
	}
	line := fmt.Sprintf("Finished %d %s", m.fin.done, noun)
	if m.fin.skipped > 0 {
		line += fmt.Sprintf(" (%d skipped)", m.fin.skipped)
	}
	body := []string{
		center(boldStyle.Render(truncWidth(m.fin.date.Format("Monday, January 2"), cw)), cw),
		center(textStyle.Render(truncWidth(line, cw)), cw),
	}
	return centeredPanel(body, fixedHelp(finishHelp, cw), m.w, m.h, gray)
}
