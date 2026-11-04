package main

import (
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// @region block:update -- MAIN SCREEN ROUTING, f / e, OVERRUN

// the main screen. with no blocks configured the app is the cycler it was
// before blocks existed.
func (m model) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.blocksOff() {
		return m.updateCycler(msg)
	}
	if m.ext != nil {
		return m.updateExtend(msg)
	}
	if m.prompt != nil && !m.inCyclerBlock() {
		return m.handleBlockPrompt(msg)
	}
	if m.prompt != nil {
		switch m.prompt.kind {
		case confirmBlockFinish, confirmStartNext:
			return m.handleBlockPrompt(msg)
		}
	}

	ph, b, idx := m.phase()
	switch ph {
	case phaseBlock:
		if b.Type == blkCycler {
			switch msg.String() {
			case "f", "e":
				return m.blockAction(msg.String(), b, idx)
			}
			return m.updateCycler(msg)
		}
		return m.updateStandardBlock(msg, b, idx)
	case phaseGap, phaseSleep:
		// before the first block and between blocks behave alike: there is a
		// next block, so it can be pulled forward
		return m.updateGap(msg, idx)
	}
	return m.updateQuiet(msg)
}

// a block that is running and can be finished or extended
func (m model) updateStandardBlock(msg tea.KeyMsg, b resolvedBlock, idx int) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.quit()
	case "?":
		m.toggleHelp()
	case "C":
		return m, m.openConfig()
	case "F":
		m.prompt = newConfirm(confirmFinish, "Finish day?")
	case "f", "e":
		return m.blockAction(msg.String(), b, idx)
	}
	return m, nil
}

// between blocks: nothing to finish, but the next one can be pulled forward
func (m model) updateGap(msg tea.KeyMsg, nextIdx int) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.quit()
	case "?":
		m.toggleHelp()
	case "C":
		return m, m.openConfig()
	case "F":
		m.prompt = newConfirm(confirmFinish, "Finish day?")
	case "f":
		bs := m.s.dayBlocks(m.c)
		if nextIdx >= 0 && nextIdx < len(bs) {
			m.prompt = newConfirm(confirmStartNext, "Start "+bs[nextIdx].Title+" early?")
		}
	}
	return m, nil
}

// sleep, day done, and an all-day stretch of free time
func (m model) updateQuiet(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.quit()
	case "?":
		m.toggleHelp()
	case "c", "C":
		return m, m.openConfig()
	}
	return m, nil
}

func (m model) blockAction(key string, b resolvedBlock, idx int) (tea.Model, tea.Cmd) {
	switch key {
	case "f":
		m.prompt = newConfirm(confirmBlockFinish, "Finish "+b.Title+" early?")
	case "e":
		m.ext = &extendState{idx: idx, step: extendStart}
	}
	return m, nil
}

func (m model) handleBlockPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	res := m.prompt.handle(msg)
	kind := m.prompt.kind
	m.prompt = nil
	if res != confirmYes {
		return m, nil
	}

	now := minuteOf(nowFn())
	_, _, idx := m.phase()
	switch kind {
	case confirmBlockFinish:
		m.s.Held = ""
		m.s.endBlockAt(m.c, idx, now)
	case confirmStartNext:
		// the block before the gap has already ended; stretching it would
		// swallow the gap rather than skip it
		m.s.cascadeFrom(m.c, idx, now)
	case confirmFinish:
		m.finishDay()
	}
	m.s.save()
	return m, m.sourcesCmd()
}

func (m model) updateExtend(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.ext = nil
	case "left", "h":
		m.ext.step = max(0, m.ext.step-1)
	case "right", "l":
		m.ext.step = min(len(extendSteps)-1, m.ext.step+1)
	case "enter":
		_, b, idx := m.phase()
		m.s.endBlockAt(m.c, idx, m.extendEnd(b))
		m.s.save()
		m.ext = nil
		return m, m.sourcesCmd()
	}
	return m, nil
}

// @region block:poll -- PHASE WATCH AND CYCLER OVERRUN

// the block the clock alone puts us in, ignoring any held block; "" when none
func (m model) clockBlockID() string {
	now := minuteOf(nowFn())
	for _, b := range m.s.dayBlocks(m.c) {
		if !b.cancelled && now >= b.start && now < b.end {
			return b.ID
		}
	}
	return ""
}

// watches for a cycler block whose end passes while its timer is still live
func (m *model) checkBlocks(now time.Time) {
	if m.blocksOff() {
		m.lastBlock = ""
		return
	}
	cur := m.clockBlockID()
	prev := m.lastBlock
	m.lastBlock = cur
	if prev == "" || prev == cur || m.s.Held != "" {
		return
	}
	b := m.c.blockByID(prev)
	if b == nil || b.Type != blkCycler {
		return
	}
	// an idle timer is nothing to interrupt; anything else is work in progress
	if m.s.Current == "" || m.s.Timer == timerIdle {
		return
	}
	m.s.Held = prev
	if !slices.Contains(m.s.Notified, prev) {
		m.s.Notified = append(m.s.Notified, prev)
		name := prev
		if it := m.c.byID(m.s.Current); it != nil {
			name = it.Name
		}
		notifyTextFn(b.Title + " ended, but " + name + " is still running. Finish or mark complete to continue.")
	}
	m.s.save()
}

// a held block ends the moment its item is finished with, and the rest of the
// day shifts up behind it
func (m *model) releaseHeld() {
	if m.s.Held == "" {
		return
	}
	id := m.s.Held
	m.s.Held = ""
	for i, b := range m.s.dayBlocks(m.c) {
		if b.ID == id {
			m.s.endBlockAt(m.c, i, minuteOf(nowFn()))
			break
		}
	}
	m.lastBlock = m.clockBlockID()
}
