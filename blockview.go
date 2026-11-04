package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// @region block:view -- TRAIL, CLOCK, PHASE SCREENS

// the block's final stretch, where the end time starts warning
const endWarnMins = 5

// the day's blocks as a trail. cur is the crumb the window scrolls to keep.
func (m model) blockCrumbs() ([]crumb, int) {
	bs := m.s.dayBlocks(m.c)
	ph, _, idx := m.phase()
	now := minuteOf(nowFn())
	// only a block actually running wears the arrow; sleep, gaps and a finished
	// day anchor the scroll on a block without making it current
	cur := -1
	if ph == phaseBlock {
		cur = idx
	}

	cs := make([]crumb, 0, len(bs))
	for i, b := range bs {
		glyph := "●"
		if b.Type == blkCycler {
			glyph = "◷"
		}
		style := lipgloss.NewStyle().Foreground(b.colorOf())
		switch {
		case b.cancelled:
			style = grayStyle.Strikethrough(true)
		case i == cur:
			glyph = "▶"
			style = style.Bold(true)
		case b.end <= now:
			style = grayStyle
		}
		cs = append(cs, crumb{glyph + fmtClockCompact(b.start) + " " + b.Title, style})
	}
	return cs, max(0, idx)
}

func (m model) renderBlockCrumbs(width int) string {
	cs, cur := m.blockCrumbs()
	return renderCrumbs(cs, cur, width, blockSep)
}

// the top row: the block trail on the left, the date on the right
func (m model) blockHeadRow(cw int) string {
	date := m.dateLabel()
	bw := max(1, cw-lipgloss.Width(date)-2)
	bc := m.renderBlockCrumbs(bw)
	return bc + spaces(max(0, cw-lipgloss.Width(bc)-lipgloss.Width(date))) + date
}

// big centered clock art. a leading zero on the hour stays gray whatever the
// block color is, so it reads as padding rather than a digit.
func clockArt(s string, style lipgloss.Style, cw, termH int) []string {
	f := pickFont(s, cw, termH)
	art := f.render(s)
	aw := f.width(s)
	left := max(0, (cw-aw)/2)

	dim := dimPrefix(f, s)

	lines := make([]string, 0, len(art))
	for _, r := range art {
		rs := []rune(r)
		if dim > 0 && dim <= len(rs) {
			lines = append(lines, spaces(left)+grayStyle.Render(string(rs[:dim]))+style.Render(string(rs[dim:])))
			continue
		}
		lines = append(lines, spaces(left)+style.Render(r))
	}
	return lines
}

// columns of leading-zero art to dim, so a padded hour reads as padding rather
// than as a digit; 0 when the hour does not start with one
func dimPrefix(f bigFont, s string) int {
	if !strings.HasPrefix(s, "0") {
		return 0
	}
	g, ok := f.glyphs['0']
	if !ok {
		return 0
	}
	return len([]rune(g[0])) + glyphGap
}

// the block's last few minutes, where the end time starts warning
func inFinalStretch(now, end int) bool { return now < end && end-now <= endWarnMins }

// start left, end right, aligned to the clock art's own edges
func clockBounds(start, end string, endStyle lipgloss.Style, artW, cw int) string {
	left := max(0, (cw-artW)/2)
	gap := max(1, artW-lipgloss.Width(start)-lipgloss.Width(end))
	return spaces(left) + grayStyle.Render(start) + spaces(gap) + endStyle.Render(end)
}

// the shared clock screen: a block, or a stretch of free time
func (m model) clockView(title string, color lipgloss.TerminalColor, start, end int, showBounds bool, status []string, help []string) string {
	w, h := m.w, m.h
	cw, ch := contentWidth(w), contentHeight(h)

	head := []string{m.blockHeadRow(cw)}
	style := lipgloss.NewStyle().Foreground(color)

	now := minuteOf(nowFn())
	clock := fmtClock(now)
	f := pickFont(clock, cw, h)
	body := clockArt(clock, style, cw, h)
	if showBounds {
		endStyle := grayStyle
		if inFinalStretch(now, end) {
			endStyle = lipgloss.NewStyle().Foreground(yellow)
		}
		body = append(body, clockBounds(fmtClock(start), fmtClock(end), endStyle, f.width(clock), cw))
	}
	body = append(body, "", center(style.Bold(true).Render(truncWidth(title, cw)), cw))
	if len(status) > 0 {
		body = append(body, "")
		body = append(body, status...)
	}

	return renderPanel(stackContent(head, body, help, ch), w, h, color)
}

// the centered, wrapped source output shared by the cycler timer and a block
func sourceRows(text string, style lipgloss.Style, cw, w, maxRows int) []string {
	if maxRows < 1 {
		return nil
	}
	srcW := max(10, min(cw, w-2-16))
	out := make([]string, 0, maxRows)
	for _, l := range wrapCapped(text, srcW, maxRows) {
		out = append(out, center(style.Render(l), cw))
	}
	return out
}

func (m model) standardBlockView(b resolvedBlock, idx int) string {
	cw := contentWidth(m.w)
	help := toggledHelp(blockHelp, m.c.HelpFull, cw)

	var status []string
	switch {
	case m.ext != nil:
		status = m.extendRows(b, idx, cw)
	case m.prompt != nil:
		status = []string{center(promptStyle.Render(m.prompt.prompt+" y/n"), cw)}
	default:
		r := m.src[b.ID]
		status = sourceRows(r.text, r.style(), cw, m.w, 2)
	}
	return m.clockView(b.Title, b.colorOf(), b.start, b.end, true, status, help)
}

// a stretch with no block in it. a bounded gap knows when it ends; a day with
// no blocks at all does not.
func (m model) freeTimeView(start, end int, bounded bool) string {
	cw := contentWidth(m.w)
	keys := freeHelp
	if bounded {
		keys = gapHelp
	}
	help := toggledHelp(keys, m.c.HelpFull, cw)
	var status []string
	if m.prompt != nil {
		status = []string{center(promptStyle.Render(m.prompt.prompt+" y/n"), cw)}
	}
	return m.clockView("FREE TIME", gray, start, end, bounded, status, help)
}

func (m model) sleepView(first resolvedBlock) string {
	cw, ch := contentWidth(m.w), contentHeight(m.h)
	head := []string{m.blockHeadRow(cw)}
	msg := fmt.Sprintf("Sleep well! %s starts at %s", first.Title, fmtClock(first.start))
	var body []string
	for _, l := range wrapText(msg, cw) {
		body = append(body, center(grayStyle.Render(l), cw))
	}
	help := toggledHelp(gapHelp, m.c.HelpFull, cw)
	return renderPanel(stackContent(head, body, help, ch), m.w, m.h, gray)
}

func (m model) dayDoneView() string {
	cw, ch := contentWidth(m.w), contentHeight(m.h)
	head := []string{m.blockHeadRow(cw)}

	done, skipped := m.s.counts()
	noun := "items"
	if done == 1 {
		noun = "item"
	}
	tail := fmt.Sprintf("Finished %d %s", done, noun)
	if skipped > 0 {
		tail += fmt.Sprintf(" (%d skipped)", skipped)
	}
	line := greenStyle.Render("Day is done!") + grayStyle.Render(" │ "+tail)
	body := []string{center(truncWidth2(line, cw), cw)}
	help := toggledHelp(doneHelp, m.c.HelpFull, cw)
	return renderPanel(stackContent(head, body, help, ch), m.w, m.h, green)
}

// a styled line is left alone unless it genuinely overflows, in which case the
// plain fallback is better than a cut that eats an escape sequence
func truncWidth2(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return grayStyle.Render(truncWidth("Day is done!", w))
}

// @region block:extend -- THE EXTEND CONTROL

var extendSteps = []int{5, 10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120}

const extendStart = 1

type extendState struct {
	idx  int
	step int
}

func fmtDur(mins int) string {
	h, m := mins/60, mins%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

// the new end a commit would write, clamped to the end of the day
func (m model) extendEnd(b resolvedBlock) int {
	return min(b.end+extendSteps[m.ext.step], minsPerDay-1)
}

func (m model) extendRows(b resolvedBlock, idx, cw int) []string {
	line := grayStyle.Render("Extend by ") + orangeStyle.Render("◀ ") +
		promptStyle.Render(fmtDur(extendSteps[m.ext.step])) + orangeStyle.Render(" ▶")
	rows := []string{center(line, cw)}

	if lost := m.s.wouldCancel(m.c, idx+1, m.extendEnd(b)); len(lost) > 0 {
		warn := "Will cancel " + lost[0]
		if len(lost) > 1 {
			warn = fmt.Sprintf("Will cancel %d blocks", len(lost))
		}
		rows = append(rows, center(redStyle.Render(warn), cw))
	}
	return rows
}
