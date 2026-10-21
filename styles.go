package main

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// @region ui:styles -- COLORS + SHARED STYLES

var (
	orange    = lipgloss.Color("#FF8000")
	yellow    = lipgloss.Color("#FFEE00")
	srcYellow = lipgloss.Color("#FFDE5C")
	green     = lipgloss.Color("#5FD700")
	red       = lipgloss.Color("#BF2846")
	gray      = lipgloss.Color("#585858")
	lightGray = lipgloss.Color("#A8A8A8")
	white     = lipgloss.Color("#FFFFFF")
	text      = lipgloss.Color("#DADADA")
	moonBlue  = lipgloss.Color("#63C2FF")
)

var (
	grayStyle      = lipgloss.NewStyle().Foreground(gray)
	lightGrayStyle = lipgloss.NewStyle().Foreground(lightGray)
	greenStyle     = lipgloss.NewStyle().Foreground(green)
	redStyle       = lipgloss.NewStyle().Foreground(red)
	orangeStyle    = lipgloss.NewStyle().Foreground(orange)
	srcStyle       = lipgloss.NewStyle().Foreground(srcYellow)
	textStyle      = lipgloss.NewStyle().Foreground(text)
	boldStyle      = lipgloss.NewStyle().Foreground(text).Bold(true)
	promptStyle    = lipgloss.NewStyle().Foreground(white).Bold(true)
	moonStyle      = lipgloss.NewStyle().Foreground(moonBlue)
)

// panel chrome consumed by the border and the left/right padding
const (
	padLeft  = 3
	padRight = 3
	minCols  = 34
	minRows  = 12
)

// full-screen panel border in the given state color. content is expected to
// already carry its own horizontal padding.
func panelStyle(w int, c lipgloss.TerminalColor) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c).
		Width(w - 2)
}

// centers a single line in w columns; a line already at least that wide is
// returned untouched
func center(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw >= w {
		return s
	}
	return spaces((w-sw)/2) + s
}

// … is East Asian Ambiguous, so it is budgeted two columns and renders one
// narrower than its reservation on a non-CJK terminal
const ellipsisCols = 2

// cuts plain text to w display columns, marking the cut with …
func truncWidth(s string, w int) string {
	if w < 1 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	head, _ := splitWidth(s, w-ellipsisCols)
	return head + "…"
}

// splits plain text at w display columns. always consumes at least one rune, so
// a grapheme wider than w cannot stall a caller looping on the remainder.
func splitWidth(s string, w int) (string, string) {
	if w < 1 {
		return "", s
	}
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		next := s[:i+size]
		if i > 0 && lipgloss.Width(next) > w {
			return s[:i], s[i:]
		}
		i += size
		if lipgloss.Width(s[:i]) >= w {
			return s[:i], s[i:]
		}
	}
	return s, ""
}

// greedy word wrap; words wider than w are split
func wrapText(s string, w int) []string {
	if w < 1 {
		return []string{s}
	}
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		cand := word
		if cur != "" {
			cand = cur + " " + word
		}
		if lipgloss.Width(cand) > w {
			if cur != "" {
				lines = append(lines, cur)
				cur = ""
			}
			for lipgloss.Width(word) > w {
				head, tail := splitWidth(word, w)
				lines = append(lines, head)
				word = tail
			}
			cur = word
			continue
		}
		cur = cand
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}

// wraps to w columns and caps at maxRows, marking the cut on the last row
func wrapCapped(s string, w, maxRows int) []string {
	if maxRows < 1 {
		return nil
	}
	lines := wrapText(s, w)
	if len(lines) <= maxRows {
		return lines
	}
	lines = lines[:maxRows]
	last := lines[maxRows-1]
	if lipgloss.Width(last)+ellipsisCols > w {
		last, _ = splitWidth(last, w-ellipsisCols)
	}
	lines[maxRows-1] = last + "…"
	return lines
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}
