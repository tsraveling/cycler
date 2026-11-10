package main

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// @region cycler:update -- TIMER CONTROLS

func (m model) updateCycler(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.prompt != nil {
		return m.handleCyclerPrompt(msg)
	}

	key := msg.String()
	if m.s.Current == "" {
		return m.updateEmptyDay(key)
	}

	switch key {
	case "q":
		return m.quit()

	case "?":
		m.toggleHelp()

	case "C":
		cmd := m.openConfig()
		return m, cmd

	case " ":
		switch m.s.Timer {
		case timerIdle, timerPaused:
			m.s.Timer = timerGoing
			m.carry = 0
			m.markRunningBlock()
			m.s.save()
		case timerGoing:
			m.s.Timer = timerPaused
			m.s.save()
		case timerDone:
			cmd := m.advance(false)
			m.startCurrent()
			return m, cmd
		}

	case "enter", "c":
		if m.s.Timer == timerDone {
			cmd := m.advance(false)
			return m, cmd
		}
		m.prompt = newConfirm(confirmComplete, "Complete early?")

	case "s":
		m.prompt = newConfirm(confirmSkip, "Skip item?")

	case "p":
		if m.canPass() {
			m.prompt = newConfirm(confirmPass, "Pass for this cycle?")
		}

	case "F":
		m.prompt = newConfirm(confirmFinish, "Finish day?")
	}
	return m, nil
}

func (m model) handleCyclerPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	res := m.prompt.handle(msg)
	kind := m.prompt.kind
	m.prompt = nil
	if res != confirmYes {
		return m, nil
	}
	switch kind {
	case confirmComplete:
		cmd := m.advance(false)
		return m, cmd
	case confirmSkip:
		cmd := m.advance(true)
		return m, cmd
	case confirmPass:
		cmd := m.passCurrent()
		return m, cmd
	case confirmFinish:
		m.finishDay()
	}
	return m, nil
}

// ends the day by hand. with blocks the log stays, because Day Done still
// counts from it; without them the cycler resets so the day can continue.
func (m *model) finishDay() {
	m.fin = finishFor(m.s)
	if m.blocksOff() {
		m.s = freshState(logicalDate(nowFn()), m.c)
	} else {
		m.s.Finished = true
		m.s.Held = ""
		// the cycler is off screen for the rest of the day, so its timer must
		// not keep counting down to a notification nothing can answer
		if m.s.Timer == timerGoing {
			m.s.Timer = timerPaused
		}
	}
	m.s.save()
	m.carry = 0
	m.scr = scrFinish
	m.rebase()
}

// nothing is left to run today, so only the day-level keys are live
func (m model) updateEmptyDay(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m.quit()
	case "c", "C":
		cmd := m.openConfig()
		return m, cmd
	case "F":
		m.prompt = newConfirm(confirmFinish, "Finish day?")
	}
	return m, nil
}

// passing only makes sense for an item that is rationed
func (m model) canPass() bool {
	it := m.c.byID(m.s.Current)
	return it != nil && it.TimesPerDay > 0
}

func (m *model) advance(skipped bool) tea.Cmd {
	m.releaseHeld()
	m.s.advance(m.c, skipped)
	m.carry = 0
	m.s.save()
	// a release hands the day straight on: the block it uncovers is announced
	// now rather than at the next poll
	m.checkPhase()
	return m.sourcesCmd()
}

// runs the item the cycle now points at, if there is one and the cycler is
// still on screen. completing an item can release a held block and hand the
// day to a standard one, and a timer started under that counts down invisibly
// to a notification nothing can answer.
func (m *model) startCurrent() {
	if m.s.Current == "" || m.s.Timer != timerIdle {
		return
	}
	if !m.blocksOff() && !m.inCyclerBlock() {
		return
	}
	m.s.Timer = timerGoing
	m.carry = 0
	m.markRunningBlock()
	m.s.save()
}

func (m *model) passCurrent() tea.Cmd {
	m.releaseHeld()
	m.s.pass(m.c)
	m.carry = 0
	m.s.save()
	m.checkPhase()
	return m.sourcesCmd()
}

// @region cycler:view -- CRUMBS, COUNTER, TIMER, SOURCE

// blinks gray/white while paused
func (m model) activeStyle() lipgloss.Style {
	switch m.s.Timer {
	case timerGoing:
		return lipgloss.NewStyle().Foreground(yellow)
	case timerDone:
		return greenStyle
	case timerPaused:
		if m.blink {
			return lipgloss.NewStyle().Foreground(white)
		}
		return grayStyle
	}
	return orangeStyle
}

func (m model) cyclerView() string {
	w, h := m.w, m.h
	if m.s.Current == "" {
		return m.emptyDayView()
	}
	cw, ch := contentWidth(w), contentHeight(h)

	help := toggledHelp(m.cyclerKeys(), m.c.HelpFull, cw)
	head := append(m.cyclerHead(cw), m.renderCounter(cw))

	region := blockRows(len(head), len(help), ch)
	art := m.timerLines(cw)

	// an open prompt or extend control has to be on screen before the timer
	// art does, because a keypress is about to answer it
	mandatory := m.ext != nil || m.prompt != nil
	status := func(rows int) []string {
		if rows < 1 {
			return nil
		}
		if m.ext != nil {
			return m.extendRows(cw, rows)
		}
		text, style := m.sourceLine()
		return sourceRows(text, style, cw, w, rows)
	}

	var block []string
	if room := region - len(art) - 1; room > 0 {
		if rows := status(room); len(rows) > 0 {
			block = append(append(slices.Clone(art), ""), rows...)
		} else {
			block = art
		}
	}
	if len(block) == 0 {
		if mandatory {
			block = status(region)
		} else {
			block = art
		}
	}

	return renderPanel(stackContent(head, block, help, ch), w, h, gray)
}

// `p pass` only belongs in the keymap when the current item is rationed
func (m model) cyclerKeys() []string {
	keys := slices.Clone(cyclerHelp)
	if m.canPass() {
		keys = slices.Insert(keys, 3, "p pass")
	}
	if m.inCyclerBlock() {
		keys = slices.Insert(keys, len(keys)-3, "f finish block", "e extend")
	}
	return keys
}

// the rows above the counter: the block trail when the day has one, then the
// cycler trail. the date rides on whichever row is on top.
func (m model) cyclerHead(cw int) []string {
	if !m.blocksOff() {
		return []string{m.blockHeadRow(cw), m.renderCrumbs(cw)}
	}
	date := m.dateLabel()
	crumbW := max(1, cw-lipgloss.Width(date)-2)
	crumbs := m.renderCrumbs(crumbW)
	return []string{crumbs + spaces(cw-lipgloss.Width(crumbs)-lipgloss.Width(date)) + date}
}

func (m model) emptyDayView() string {
	cw, ch := contentWidth(m.w), contentHeight(m.h)
	help := fixedHelp(emptyDayHelp, cw)
	if m.prompt != nil {
		help = []string{promptStyle.Render(m.prompt.prompt + " y/n")}
	}
	var body []string
	for _, l := range wrapText("The cycler is empty. Enjoy your day off!", cw) {
		body = append(body, center(greenStyle.Render(l), cw))
	}
	if rows := m.extendRows(cw, 2); len(rows) > 0 {
		body = append(body, "")
		body = append(body, rows...)
	}
	var head []string
	if !m.blocksOff() {
		head = []string{m.blockHeadRow(cw)}
	}
	return renderPanel(stackContent(head, body, help, ch), m.w, m.h, gray)
}

func (m model) timerLines(cw int) []string {
	it := m.c.byID(m.s.Current)
	if it == nil {
		return nil
	}
	if m.s.Timer == timerDone {
		return timerBlock("DONE", fmtTime(it.Seconds), greenStyle, cw, m.h)
	}
	return timerBlock(fmtTime(m.s.Remaining), fmtTime(it.Seconds), m.activeStyle(), cw, m.h)
}

// the line under the timer: an open prompt, the upcoming item once this one
// is done, or the current item's source
func (m model) sourceLine() (string, lipgloss.Style) {
	if m.prompt != nil {
		return m.prompt.prompt + " y/n", promptStyle
	}
	if m.s.Held != "" {
		// today's end, which an earlier extend or cascade may have moved
		for _, b := range m.s.dayBlocks(m.c) {
			if b.ID == m.s.Held {
				return "Block ended at " + fmtClock(b.end) + " — finish to continue", redStyle
			}
		}
	}
	if m.s.Timer == timerDone {
		if up := m.s.upcoming(m.c); up >= 0 {
			next := m.c.Items[up]
			r := m.src[next.ID]
			return "Next: " + next.Name + ": " + r.text, r.style()
		}
		return "Last one today", grayStyle
	}
	r := m.src[m.s.Current]
	return r.text, r.style()
}

func (m model) dateLabel() string {
	return grayStyle.Render(nowFn().Format("Mon, Jan 2"))
}

const (
	crumbSep = " -> "
	blockSep = " - "
)

// the glyphs used in crumbs and the counter are East Asian Ambiguous, so each
// crumb budgets one spare column for terminals that draw them wide
const crumbSlack = 1

// cols reserved for a "‹ " or " ›" scroll marker; distinct from the ▶ current icon
const markerCols = 3

type crumb struct {
	text  string
	style lipgloss.Style
}

// only the items this cycle is made of: what can still run today, plus what
// has already been settled in the cycle so the trail doesn't shed items as
// they hit their daily limit
func (m model) crumbs() ([]crumb, int) {
	cs := make([]crumb, 0, len(m.c.Items))
	cur := 0
	for _, it := range m.c.Items {
		done, passed := m.s.doneThisCycle(it.ID), m.s.passedThisCycle(it.ID)
		if it.ID != m.s.Current && !done && !passed && !m.s.eligible(it) {
			continue
		}
		switch {
		case it.ID == m.s.Current:
			cur = len(cs)
			cs = append(cs, crumb{"▶ " + it.Name, m.activeStyle().Bold(true)})
		case done:
			cs = append(cs, crumb{"✓ " + it.Name, grayStyle})
		case passed:
			cs = append(cs, crumb{"~ " + it.Name, grayStyle})
		default:
			cs = append(cs, crumb{"○ " + it.Name, lightGrayStyle})
		}
	}
	return cs, cur
}

func crumbWidth(c crumb) int { return lipgloss.Width(c.text) + crumbSlack }

// last index fully visible when rendering from off, given a column budget
func crumbEnd(cs []crumb, off, budget int, sep string) int {
	w := 0
	end := off - 1
	for i := off; i < len(cs); i++ {
		add := crumbWidth(cs[i])
		if i > off {
			add += lipgloss.Width(sep)
		}
		if w+add > budget {
			break
		}
		w += add
		end = i
	}
	return end
}

// budget for a given offset, discounting the markers that will be drawn
func crumbBudget(cs []crumb, off, width int, sep string) int {
	budget := width
	if off > 0 {
		budget -= markerCols
	}
	if crumbEnd(cs, off, budget, sep) < len(cs)-1 {
		budget -= markerCols
	}
	return budget
}

// smallest offset that still shows the current item and the next one, so the
// trail back-fills leftward with as much history as fits
func autoOffset(cs []crumb, cur, width int, sep string) int {
	need := min(cur+1, len(cs)-1)
	for off := 0; off <= cur; off++ {
		if crumbEnd(cs, off, crumbBudget(cs, off, width, sep), sep) >= need {
			return off
		}
	}
	return cur
}

func (m model) renderCrumbs(width int) string {
	cs, cur := m.crumbs()
	return renderCrumbs(cs, cur, width, crumbSep)
}

// the shared trail renderer: windows the crumbs around cur and marks the cuts
func renderCrumbs(cs []crumb, cur, width int, sep string) string {
	if len(cs) == 0 {
		return ""
	}
	off := autoOffset(cs, cur, width, sep)
	budget := crumbBudget(cs, off, width, sep)
	end := crumbEnd(cs, off, budget, sep)

	// nothing fits whole, so cut the one crumb to the budget rather than
	// letting a wide label wrap out of the panel
	lone := end < off
	if lone {
		end = off
	}

	var sb strings.Builder
	if off > 0 {
		sb.WriteString(grayStyle.Render("‹ "))
	}
	for i := off; i <= end; i++ {
		if i > off {
			sb.WriteString(grayStyle.Render(sep))
		}
		txt := cs[i].text
		if lone {
			txt = truncWidth(txt, budget-crumbSlack)
		}
		sb.WriteString(cs[i].style.Render(txt))
	}
	if end < len(cs)-1 {
		sb.WriteString(grayStyle.Render(" ›"))
	}
	return sb.String()
}

// one dot per advanced item today, then the active dot. the last item of a
// cycle gets a large dot, skips are hollow.
func (m model) renderCounter(cw int) string {
	type dot struct {
		g     string
		style lipgloss.Style
	}
	dots := make([]dot, 0, len(m.s.Log)+1)
	for _, e := range m.s.Log {
		dots = append(dots, dot{counterGlyph(e.Skipped, e.Last), grayStyle})
	}
	if m.c.indexOf(m.s.Current) >= 0 {
		dots = append(dots, dot{counterGlyph(false, m.s.nextOpen(m.c) < 0), m.activeStyle()})
	}

	// each dot is budgeted two cols: the glyph plus its separator
	fit := cw / 2
	start := 0
	if len(dots) > fit {
		start = len(dots) - max(0, fit-1)
	}

	parts := make([]string, 0, len(dots)-start+1)
	if start > 0 {
		parts = append(parts, grayStyle.Render("‹"))
	}
	for _, d := range dots[start:] {
		parts = append(parts, d.style.Render(d.g))
	}
	return strings.Join(parts, " ")
}

func counterGlyph(skipped, last bool) string {
	switch {
	case skipped && last:
		return "○"
	case skipped:
		return "◦"
	case last:
		return "●"
	}
	return "•"
}
