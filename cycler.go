package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// @region cycler:update -- TIMER CONTROLS

func (m model) updateCycler(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.prompt != nil {
		return m.handleCyclerPrompt(msg)
	}

	switch msg.String() {
	case "q":
		return m.quit()

	case "?":
		m.toggleHelp()

	case "c", "C":
		cmd := m.openConfig()
		return m, cmd

	case " ":
		switch m.s.Timer {
		case timerIdle, timerPaused:
			m.s.Timer = timerGoing
			m.carry = 0
			m.s.save()
		case timerGoing:
			m.s.Timer = timerPaused
			m.s.save()
		case timerDone:
			cmd := m.advance(false)
			return m, cmd
		}

	case "enter":
		if m.s.Timer == timerDone {
			cmd := m.advance(false)
			return m, cmd
		}
		m.prompt = newConfirm(confirmComplete, "Complete early?")

	case "s":
		m.prompt = newConfirm(confirmSkip, "Skip item?")

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
	case confirmFinish:
		m.fin = finishFor(m.s)
		m.s = freshState(logicalDate(nowFn()), m.c)
		m.s.save()
		m.carry = 0
		m.scr = scrFinish
	}
	return m, nil
}

func (m *model) advance(skipped bool) tea.Cmd {
	m.s.advance(m.c, skipped)
	m.carry = 0
	m.s.save()
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
	cw, ch := contentWidth(w), contentHeight(h)

	help := toggledHelp(cyclerHelp, m.c.HelpFull, cw)

	date := m.dateLabel()
	crumbW := max(1, cw-lipgloss.Width(date)-2)
	crumbs := m.renderCrumbs(crumbW)
	row0 := crumbs + spaces(cw-lipgloss.Width(crumbs)-lipgloss.Width(date)) + date
	head := []string{row0, m.renderCounter(cw)}

	region := blockRows(len(head), len(help), ch)
	block := m.timerLines(cw)
	if room := region - len(block) - 1; room > 0 {
		text, style := m.sourceLine()
		srcW := max(10, min(cw, w-2-16))
		block = append(block, "")
		for _, l := range wrapCapped(text, srcW, room) {
			block = append(block, center(style.Render(l), cw))
		}
	}

	return renderPanel(stackContent(head, block, help, ch), w, h, gray)
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
	if m.s.Timer == timerDone {
		next := m.c.Items[m.s.upcoming(m.c)]
		r := m.src[next.ID]
		return "Next: " + next.Name + ": " + r.text, r.style()
	}
	r := m.src[m.s.Current]
	return r.text, r.style()
}

// today in gray, or the calendar date with a moon when working past midnight
// into the previous logical day
func (m model) dateLabel() string {
	now := nowFn()
	d := now.Format("Mon, Jan 2")
	if now.Format(dateLayout) != logicalDate(now) {
		return moonStyle.Render("☾ " + d)
	}
	return grayStyle.Render(d)
}

const crumbSep = " -> "

// the glyphs used in crumbs and the counter are East Asian Ambiguous, so each
// crumb budgets one spare column for terminals that draw them wide
const crumbSlack = 1

// cols reserved for a "‹ " or " ›" scroll marker; distinct from the ▶ current icon
const markerCols = 3

type crumb struct {
	text  string
	style lipgloss.Style
}

func (m model) crumbs() ([]crumb, int) {
	cur := m.c.indexOf(m.s.Current)
	cs := make([]crumb, len(m.c.Items))
	for i, it := range m.c.Items {
		switch {
		case i == cur:
			cs[i] = crumb{"▶ " + it.Name, m.activeStyle().Bold(true)}
		case m.s.doneThisCycle(it.ID):
			cs[i] = crumb{"✓ " + it.Name, grayStyle}
		default:
			cs[i] = crumb{"○ " + it.Name, lightGrayStyle}
		}
	}
	return cs, max(0, cur)
}

func crumbWidth(c crumb) int { return lipgloss.Width(c.text) + crumbSlack }

// last index fully visible when rendering from off, given a column budget
func crumbEnd(cs []crumb, off, budget int) int {
	w := 0
	end := off - 1
	for i := off; i < len(cs); i++ {
		add := crumbWidth(cs[i])
		if i > off {
			add += lipgloss.Width(crumbSep)
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
func crumbBudget(cs []crumb, off, width int) int {
	budget := width
	if off > 0 {
		budget -= markerCols
	}
	if crumbEnd(cs, off, budget) < len(cs)-1 {
		budget -= markerCols
	}
	return budget
}

// smallest offset that still shows the current item and the next one, so the
// trail back-fills leftward with as much history as fits
func autoOffset(cs []crumb, cur, width int) int {
	need := min(cur+1, len(cs)-1)
	for off := 0; off <= cur; off++ {
		if crumbEnd(cs, off, crumbBudget(cs, off, width)) >= need {
			return off
		}
	}
	return cur
}

func (m model) renderCrumbs(width int) string {
	cs, cur := m.crumbs()
	if len(cs) == 0 {
		return ""
	}
	off := autoOffset(cs, cur, width)
	budget := crumbBudget(cs, off, width)
	end := crumbEnd(cs, off, budget)

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
			sb.WriteString(grayStyle.Render(crumbSep))
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
