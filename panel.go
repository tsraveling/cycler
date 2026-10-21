package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// @region ui:panel -- FULL-SCREEN PANEL LAYOUT

func contentWidth(w int) int  { return w - 2 - padLeft - padRight }
func contentHeight(h int) int { return h - 2 - 1 } // borders + one line of top padding

func tooSmall(w, h int) bool { return w < minCols || h < minRows }

func tooSmallView(w, h int) string {
	msg := grayStyle.Render("terminal too small")
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, msg)
}

// assembles the panel: one blank line of top padding, then exactly
// contentHeight(h) rows, each inset by padLeft. callers must keep every line
// within contentWidth(w) so nothing wraps and blows the height.
func renderPanel(lines []string, w, h int, c lipgloss.TerminalColor) string {
	ch := contentHeight(h)
	rows := make([]string, 0, ch+1)
	rows = append(rows, "")
	for i := 0; i < ch; i++ {
		s := ""
		if i < len(lines) {
			s = lines[i]
		}
		rows = append(rows, spaces(padLeft)+s)
	}
	return panelStyle(w, c).Render(strings.Join(rows, "\n"))
}

// a centered body in a plain bordered panel, for the simple full-screen states
func centeredPanel(body []string, help []string, w, h int, c lipgloss.TerminalColor) string {
	ch := contentHeight(h)
	return renderPanel(stackContent(nil, body, help, ch), w, h, c)
}

// M:SS with no leading zero on the minutes
func fmtTime(secs int) string {
	if secs < 0 {
		secs = 0
	}
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

// big centered art plus the gray denominator, right-aligned under it
func timerBlock(big string, denom string, style lipgloss.Style, cw, termH int) []string {
	f := pickFont(big, cw, termH)
	art := f.render(big)
	aw := f.width(big)
	left := max(0, (cw-aw)/2)

	lines := make([]string, 0, f.rows+1)
	for _, r := range art {
		lines = append(lines, spaces(left)+style.Render(r))
	}
	d := "/ " + denom
	lines = append(lines, spaces(left+max(0, aw-len(d)))+grayStyle.Render(d))
	return lines
}

// lays out the content rows: header rows on top, help at the bottom with one
// blank row above the border, block centered in what is left
func stackContent(head []string, block []string, help []string, ch int) []string {
	lines := make([]string, ch)
	copy(lines, head)

	helpAt := ch - 1 - len(help)
	for i, hl := range help {
		if helpAt+i >= len(head) && helpAt+i < ch {
			lines[helpAt+i] = hl
		}
	}

	regionTop := len(head)
	regionH := max(0, helpAt-regionTop)
	top := regionTop + max(0, (regionH-len(block))/2)
	for i, bl := range block {
		if top+i >= regionTop && top+i < helpAt {
			lines[top+i] = bl
		}
	}
	return lines
}

// rows available to a centered block once head, help and the blank above the
// help are paid for
func blockRows(headN, helpN, ch int) int {
	return max(0, ch-1-helpN-1-headN)
}
