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

// start left, end right, the countdown centered between them, all aligned to
// the clock art's own edges. the countdown is dropped when it would crowd the
// bounds rather than squeezed in.
func clockBounds(start, end, mid string, endStyle, midStyle lipgloss.Style, artW, cw int) string {
	left := max(0, (cw-artW)/2)
	slack := artW - lipgloss.Width(start) - lipgloss.Width(end)
	if mw := lipgloss.Width(mid); mid != "" && slack-mw >= 2 {
		lg := (slack - mw) / 2
		rg := slack - mw - lg
		return spaces(left) + grayStyle.Render(start) + spaces(lg) +
			midStyle.Render(mid) + spaces(rg) + endStyle.Render(end)
	}
	return spaces(left) + grayStyle.Render(start) + spaces(max(1, slack)) + endStyle.Render(end)
}

// seconds left as 1:03:10, 5:04 or 52s, with the style it reads in: gray until
// the last minute, then yellow. empty once the time is up.
func fmtRemaining(secs int) (string, lipgloss.Style) {
	switch {
	case secs <= 0:
		return "", grayStyle
	case secs < 60:
		return fmt.Sprintf("%ds", secs), lipgloss.NewStyle().Foreground(yellow)
	case secs < 3600:
		return fmt.Sprintf("%d:%02d", secs/60, secs%60), grayStyle
	}
	return fmt.Sprintf("%d:%02d:%02d", secs/3600, (secs/60)%60, secs%60), grayStyle
}

// the shared clock screen: a block, or a stretch of free time.
//
// status is asked for the rows it may use rather than handed a fixed list,
// because a prompt or an open control has to be on screen before the clock art
// does — a keypress is about to answer it. the art is what yields on a short
// panel, then the start/end row, then the spacer.
func (m model) clockView(title string, color lipgloss.TerminalColor, start, end int, showBounds, showRemain bool, status func(int) []string, help []string) string {
	w, h := m.w, m.h
	cw, ch := contentWidth(w), contentHeight(h)

	head := []string{m.blockHeadRow(cw)}
	region := blockRows(len(head), len(help), ch)
	style := lipgloss.NewStyle().Foreground(color)

	now := minuteOf(nowFn())
	clock := fmtClock(now)
	f := pickFont(clock, cw, h)
	art := clockArt(clock, style, cw, h)

	boundsRow := ""
	if showBounds {
		endStyle := grayStyle
		if inFinalStretch(now, end) {
			endStyle = lipgloss.NewStyle().Foreground(yellow)
		}
		remain, remainStyle := "", grayStyle
		if showRemain {
			remain, remainStyle = fmtRemaining(end*60 - secondOf(nowFn()))
		}
		boundsRow = clockBounds(fmtClock(start), fmtClock(end), remain, endStyle, remainStyle, f.width(clock), cw)
	}
	titleRow := center(style.Bold(true).Render(truncWidth(title, cw)), cw)

	// the status is sized first and then never dropped: the ladder below sheds
	// the art around it, rather than shedding the thing a keypress will answer
	budget := max(0, min(2, region-2))
	if budget == 0 && (m.ext != nil || m.prompt != nil) {
		budget = 1
	}
	statusRows := status(budget)

	assemble := func(withArt, withBounds, withGap bool) []string {
		var out []string
		if withArt {
			out = append(out, art...)
		}
		if withBounds && boundsRow != "" {
			out = append(out, boundsRow)
		}
		if withGap && len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, titleRow)
		if len(statusRows) > 0 {
			if withGap {
				out = append(out, "")
			}
			out = append(out, statusRows...)
		}
		return out
	}

	var body []string
	for _, opt := range [][3]bool{
		{true, true, true},
		{true, true, false},
		{true, false, false},
		{false, false, false},
	} {
		body = assemble(opt[0], opt[1], opt[2])
		if len(body) <= region {
			break
		}
	}
	return renderPanel(stackContent(head, body, help, ch), w, h, color)
}

// a confirm, centered and cut to the panel. an untruncated block title can
// otherwise push the prompt wider than the panel and wrap the whole layout.
func promptRow(c *confirm, cw int) string {
	const tail = " y/n"
	body := truncWidth(c.prompt, max(1, cw-len(tail)))
	return center(promptStyle.Render(body+tail), cw)
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

func (m model) standardBlockView(b resolvedBlock) string {
	cw := contentWidth(m.w)
	help := toggledHelp(blockHelp, m.c.HelpFull, cw)

	status := func(rows int) []string {
		switch {
		case m.ext != nil:
			return m.extendRows(cw, rows)
		case m.prompt != nil:
			return []string{promptRow(m.prompt, cw)}
		default:
			r := m.src[b.ID]
			return sourceRows(r.text, r.style(), cw, m.w, min(rows, 2))
		}
	}
	return m.clockView(b.Title, b.colorOf(), b.start, b.end, true, true, status, help)
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
	status := func(int) []string {
		if m.prompt == nil {
			return nil
		}
		return []string{promptRow(m.prompt, cw)}
	}
	return m.clockView("FREE TIME", gray, start, end, bounded, false, status, help)
}

func (m model) sleepView(first resolvedBlock) string {
	cw, ch := contentWidth(m.w), contentHeight(m.h)
	head := []string{m.blockHeadRow(cw)}
	msg := fmt.Sprintf("Sleep well! %s starts at %s", first.Title, fmtClock(first.start))
	var body []string
	for _, l := range wrapText(msg, cw) {
		body = append(body, center(grayStyle.Render(l), cw))
	}
	if m.prompt != nil {
		body = append(body, "", promptRow(m.prompt, cw))
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
	if m.prompt != nil {
		body = append(body, "", promptRow(m.prompt, cw))
	}
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
	// the block itself, not its position: an index goes stale the moment the
	// day's boundaries move or the clock rolls into the next block
	id   string
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

// the block the open control is acting on, found by identity
func (m model) extendTarget() (resolvedBlock, int, bool) {
	if m.ext == nil {
		return resolvedBlock{}, -1, false
	}
	for i, b := range m.s.dayBlocks(m.c) {
		if b.ID == m.ext.id && !b.cancelled {
			return b, i, true
		}
	}
	return resolvedBlock{}, -1, false
}

// the control only stands while its own block is the one running. adjacent
// blocks hand over without passing through a gap, so the phase alone is not
// enough to tell that the block it was opened on has ended.
func (m model) extendLive() bool {
	if m.ext == nil {
		return false
	}
	ph, b, _ := m.phase()
	return ph == phaseBlock && b.ID == m.ext.id
}

// where an extension is measured from. a held block is already past its end,
// so its step runs from now — otherwise a short step lands in the past and
// changes nothing visible.
func (m model) extendFrom(b resolvedBlock) int {
	from := b.end
	if now := minuteOf(nowFn()); m.s.Held == b.ID && now > from {
		from = now
	}
	return from
}

// the new end a commit would write, clamped to the end of the day
func (m model) extendEnd(b resolvedBlock) int {
	return min(m.extendFrom(b)+extendSteps[m.ext.step], minsPerDay-1)
}

// maxRows is the panel's budget. the warning is never the row that gets
// dropped: a step that destroys blocks has to say so before enter commits it.
func (m model) extendRows(cw, maxRows int) []string {
	b, idx, ok := m.extendTarget()
	if !ok || maxRows < 1 {
		return nil
	}
	// the effective step, not the chosen one: against the end of the day they
	// differ, and a stepper that keeps counting while nothing moves is a lie
	stepper := grayStyle.Render("Extend by ") + orangeStyle.Render("◀ ") +
		promptStyle.Render(fmtDur(m.extendEnd(b)-m.extendFrom(b))) + orangeStyle.Render(" ▶")

	lost := m.s.wouldCancel(m.c, idx+1, m.extendEnd(b))
	if len(lost) == 0 {
		return []string{center(stepper, cw)}
	}
	warn := "Will cancel " + lost[0]
	if len(lost) > 1 {
		warn = fmt.Sprintf("Will cancel %d blocks", len(lost))
	}
	if maxRows < 2 {
		return []string{center(stepper+grayStyle.Render(" · ")+redStyle.Render(warn), cw)}
	}
	return []string{center(stepper, cw), center(redStyle.Render(warn), cw)}
}
