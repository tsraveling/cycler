package main

import (
	tea "github.com/charmbracelet/bubbletea"
)

// @region block:update -- MAIN SCREEN ROUTING, f / e, OVERRUN

// the main screen. with no blocks configured the app is the cycler it was
// before blocks existed.
func (m model) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.blocksOff() {
		return m.updateCycler(msg)
	}
	// the control cannot outlive the block it was opened on
	if m.ext != nil {
		if m.extendLive() {
			return m.updateExtend(msg)
		}
		m.ext = nil
	}
	// routed by what the prompt is, not by where the clock has since moved: a
	// cycler prompt answered after its block ended must still be answered
	if m.prompt != nil {
		switch m.prompt.kind {
		case confirmBlockFinish, confirmStartNext:
			return m.handleBlockPrompt(msg)
		}
		return m.handleCyclerPrompt(msg)
	}

	ph, b, idx := m.phase()
	switch ph {
	case phaseBlock:
		if b.Type == blkCycler {
			// the ad-hoc cycler has no scheduled end, so there is nothing to
			// finish early or extend
			if b.ID != openCyclerID {
				switch msg.String() {
				case "f", "e":
					return m.blockAction(msg.String(), b, idx)
				}
			}
			return m.updateCycler(msg)
		}
		return m.updateStandardBlock(msg, b, idx)
	case phaseNoBlocks:
		return m.updateFreeDay(msg)
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
			m.prompt = newBlockConfirm(confirmStartNext, "Start "+bs[nextIdx].Title+" early?", nextIdx)
		}
	}
	return m, nil
}

// a day with no blocks of its own: free time until the cycler is started by
// hand, which runs it open-ended until F or midnight
func (m model) updateFreeDay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.quit()
	case "?":
		m.toggleHelp()
	case "C":
		return m, m.openConfig()
	case "c":
		m.s.OpenCycler = true
		m.s.OpenCyclerAt = minuteOf(nowFn())
		m.s.reconcile(m.c)
		m.s.save()
		m.rebase()
		return m, m.sourcesCmd()
	}
	return m, nil
}

// day done, and the stretch before the first block
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
		m.prompt = newBlockConfirm(confirmBlockFinish, "Finish "+b.Title+" early?", idx)
	case "e":
		m.ext = &extendState{id: b.ID, step: extendStart}
	}
	return m, nil
}

func (m model) handleBlockPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	res := m.prompt.handle(msg)
	kind, idx := m.prompt.kind, m.prompt.idx
	m.prompt = nil
	if res != confirmYes {
		return m, nil
	}

	now := minuteOf(nowFn())
	bs := m.s.dayBlocks(m.c)
	if idx < 0 || idx >= len(bs) {
		return m, nil
	}
	switch kind {
	case confirmBlockFinish:
		at := now
		// a block that ran out while the prompt was open is finished, not
		// stretched; a held one is already past its end by design
		if m.s.Held != bs[idx].ID && at > bs[idx].end {
			at = bs[idx].end
		}
		m.s.Held, m.s.Running = "", ""
		// a cycler block taken off screen leaves its timer counting down to a
		// notification with nothing to answer it
		if bs[idx].Type == blkCycler && m.s.Timer == timerGoing {
			m.s.Timer = timerPaused
		}
		m.s.endBlockAt(m.c, idx, at)
	case confirmStartNext:
		// the block before the gap has already ended; stretching it would
		// swallow the gap rather than skip it
		m.s.startBlockAt(m.c, idx, now)
	}
	m.s.save()
	m.rebase()
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
		// the block the control was opened on, not whatever is current now
		b, idx, ok := m.extendTarget()
		if ok {
			end := m.extendEnd(b)
			m.s.endBlockAt(m.c, idx, end)
			m.syncHold(b.ID, end)
			m.s.save()
		}
		m.ext = nil
		m.rebase()
		return m, m.sourcesCmd()
	}
	return m, nil
}

// @region block:poll -- PHASE WATCH AND CYCLER OVERRUN

// records which cycler block a timer belongs to the moment it starts, so the
// overrun is detectable later without having to catch the transition
func (m *model) markRunningBlock() {
	if m.blocksOff() {
		return
	}
	if ph, b, _ := m.phase(); ph == phaseBlock && b.Type == blkCycler {
		m.s.Running = b.ID
	}
}

// watches for a cycler block whose end passes while its timer is still live
func (m *model) checkBlocks() {
	if m.blocksOff() {
		m.s.Running = ""
		return
	}
	if m.ext != nil && !m.extendLive() {
		m.ext = nil
	}
	// a day ended by hand has nothing left to overrun
	if m.s.Finished {
		return
	}

	now := minuteOf(nowFn())
	bs := m.s.dayBlocks(m.c)
	live := m.s.Current != "" && m.s.Timer != timerIdle

	// an idle timer is nothing to interrupt; anything else is work in progress.
	// a hold cannot outlive the work it was holding for — the item may have
	// been edited or deleted out from under it.
	if !live {
		m.s.Running = ""
		if m.s.Held != "" {
			m.s.Held = ""
			m.s.save()
		}
		return
	}
	for _, b := range bs {
		if !b.cancelled && b.Type == blkCycler && now >= b.start && now < b.end {
			m.s.Running = b.ID
		}
	}
	if m.s.Held != "" || m.s.Running == "" {
		return
	}

	for _, b := range bs {
		if b.ID != m.s.Running {
			continue
		}
		if b.cancelled || now < b.end {
			return
		}
		// the Held guard above already stops this firing every poll, so a
		// second overrun of the same block after an extend still announces
		// itself rather than being suppressed for the rest of the day
		m.s.Held = b.ID
		name := b.Title
		if it := m.c.byID(m.s.Current); it != nil {
			name = it.Name
		}
		notifyTextFn(b.Title + " ended, but " + name + " is still running. Finish or mark complete to continue.")
		m.s.save()
		return
	}
}

// @region block:watch -- BOUNDARY NOTIFICATIONS

// what the phase watcher last saw
type phaseMark struct {
	ph dayPhase
	// the live block for phaseBlock, the one coming up for sleep and gap,
	// nothing for the phases that concern no block
	id, title string
}

func markOf(ph dayPhase, b resolvedBlock) phaseMark {
	switch ph {
	case phaseBlock, phaseGap, phaseSleep:
		return phaseMark{ph: ph, id: b.ID, title: b.Title}
	}
	return phaseMark{ph: ph}
}

// moves the baseline without announcing anything. every boundary the user
// moved by hand lands here: they were watching the screen when it moved.
func (m *model) rebase() {
	ph, b, _ := m.phase()
	m.mark = markOf(ph, b)
}

// announces a boundary the clock crossed. a held cycler block still counts as
// running, so its end is the overrun warning's to report, not this one's.
func (m *model) checkPhase() {
	switch m.scr {
	case scrConfig, scrForm, scrFinish:
		// a day being edited or summarised has no boundaries worth calling out,
		// and the edit itself must not read as one
		m.rebase()
		return
	}

	ph, b, _ := m.phase()
	cur := markOf(ph, b)
	if cur.ph == m.mark.ph && cur.id == m.mark.id {
		return
	}
	prev := m.mark
	m.mark = cur

	// a block going into overrun outranks the clock and so reads as a block
	// starting. it is the overrun warning's to report; the release that ends
	// it still announces what the day hands over to
	if ph == phaseBlock && b.ID == m.s.Held {
		return
	}

	// the ad-hoc cycler ends at midnight, where the rollover speaks instead
	if prev.ph == phaseBlock && prev.id == openCyclerID {
		return
	}
	switch {
	case prev.ph != phaseBlock:
		// nothing ended, so only a block beginning is worth saying
		if ph == phaseBlock {
			notifyTextFn("Starting " + cur.title + ".")
		}
	case ph == phaseBlock:
		notifyTextFn(prev.title + " finished. Starting " + cur.title + ".")
	case ph == phaseGap || ph == phaseSleep:
		// how long the wait actually is, not how long it was scheduled to be:
		// a block released early leaves more free time than its gap
		free := max(0, b.start-minuteOf(nowFn()))
		notifyTextFn(prev.title + " finished. You have " + fmtDur(free) +
			" of free time before " + cur.title + ".")
	case ph == phaseDone:
		notifyTextFn(prev.title + " finished. Day is done!")
	}
}

// a boundary that moved may have given a held block more time, in which case
// it is running again rather than overrunning
func (m *model) syncHold(id string, newEnd int) {
	if m.s.Held == id && newEnd > minuteOf(nowFn()) {
		m.s.Held = ""
	}
}

// a held block ends the moment its item is finished with, and the rest of the
// day shifts up behind it
func (m *model) releaseHeld() {
	if m.s.Held == "" {
		return
	}
	id := m.s.Held
	m.s.Held, m.s.Running = "", ""
	for i, b := range m.s.dayBlocks(m.c) {
		if b.ID == id {
			m.s.endBlockAt(m.c, i, minuteOf(nowFn()))
			break
		}
	}
}
