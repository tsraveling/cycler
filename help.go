package main

import "github.com/charmbracelet/lipgloss"

// @region ui:help -- HELPLINE

const maxHelpLines = 3

var (
	firstRunHelp = []string{"c configure", "q quit"}
	finishHelp   = []string{"press any key"}
	cyclerHelp   = []string{
		"space start/pause", "enter/c complete", "s skip", "F finish day",
		"C configure", "? help", "q quit",
	}
	configHelp = []string{
		"a add", "o insert", "e/enter edit", "j/k move", "J/K reorder", "d delete",
		"tab blocks", "esc back", "? help",
	}
	configEmptyHelp = []string{"a to add first item", "tab blocks"}
	blocksHelp      = []string{
		"a add", "o insert", "e/enter edit", "j/k move", "d delete",
		"tab cycler", "esc back", "? help",
	}
	blocksEmptyHelp = []string{"a to add first block", "tab cycler"}
	emptyDayHelp    = []string{"c configure", "F finish day", "q quit"}
	formHelp        = []string{
		"j/k move", "enter edit", "tab next", "h/l change", "space toggle",
		"esc cancel", "? help",
	}
	blockHelp = []string{
		"f finish block", "e extend", "F finish day", "C configure", "? help", "q quit",
	}
	doneHelp = []string{"C configure", "q quit"}
	gapHelp  = []string{"f start next", "F finish day", "C configure", "? help", "q quit"}
	freeHelp = []string{"C configure", "q quit"}
)

const helpSep = "   "

// greedily packs entries into lines no wider than width
func wrapHelp(entries []string, width int) []string {
	var lines []string
	cur := ""
	for _, e := range entries {
		cand := e
		if cur != "" {
			cand = cur + helpSep + e
		}
		if lipgloss.Width(cand) > width && cur != "" {
			lines = append(lines, cur)
			cur = e
			continue
		}
		cur = cand
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func styleHelp(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = grayStyle.Render(l)
	}
	return out
}

// always-on helpline, for screens with only a couple of keys
func fixedHelp(entries []string, cw int) []string {
	return styleHelp(wrapHelp(entries, cw))
}

// the collapsible helpline: the full keymap when open and it fits, otherwise a
// right-aligned `? help`
func toggledHelp(entries []string, full bool, cw int) []string {
	if full {
		if lines := wrapHelp(entries, cw); len(lines) <= maxHelpLines {
			return styleHelp(lines)
		}
	}
	return []string{spaces(max(0, cw-len("? help"))) + grayStyle.Render("? help")}
}
