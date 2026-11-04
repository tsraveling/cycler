package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// @region config:blocks -- BLOCK ADMIN TAB

type configTab int

const (
	tabCycler configTab = iota
	tabBlocks
)

// fixed columns; the rest of the row is shared out right to left
const (
	colTime = 9
	colDays = 20
	colGap  = 2
	colType = 14
	// below this much remainder a column is dropped rather than squeezed
	colMin = 10
)

func (m model) updateBlocks(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.prompt != nil {
		if m.prompt.handle(msg) == confirmYes {
			m.deleteSelectedBlock()
		}
		m.prompt = nil
		return m, nil
	}

	n := len(m.c.Blocks)
	switch msg.String() {
	case "tab":
		m.tab = tabCycler
		return m, nil
	case "esc":
		cmd := m.leaveConfig()
		return m, cmd
	case "?":
		m.toggleHelp()
		return m, nil
	case "a":
		return m, m.openBlockForm(-1, -1, nil)
	}
	if n == 0 {
		return m, nil
	}

	switch msg.String() {
	case "e", "enter":
		return m, m.openBlockForm(m.selBlock, -1, nil)
	case "o":
		b := m.c.Blocks[m.selBlock]
		return m, m.openBlockForm(-1, b.End, b.ActiveDays)
	case "up", "k":
		m.selBlock = max(0, m.selBlock-1)
	case "down", "j":
		m.selBlock = min(n-1, m.selBlock+1)
	case "d":
		m.prompt = newConfirm(confirmDelete, "Delete? y/n")
	}
	return m, nil
}

func (m *model) deleteSelectedBlock() {
	i := m.selBlock
	if i < 0 || i >= len(m.c.Blocks) {
		return
	}
	m.s.dropBlockOverride(m.c.Blocks[i].ID)
	m.c.Blocks = append(m.c.Blocks[:i], m.c.Blocks[i+1:]...)
	m.c.save()
	m.s.save()
	m.selBlock = clamp(m.selBlock, 0, max(0, len(m.c.Blocks)-1))
}

// the tab strip that heads the config screen
func (m model) tabHeader() string {
	on, off := orangeStyle.Bold(true), grayStyle
	left, right := on, off
	if m.tab == tabBlocks {
		left, right = off, on
	}
	return left.Render("Cycler Config") + grayStyle.Render(" │ ") + right.Render("Block Admin")
}

func (m model) blockListView() string {
	w, h := m.w, m.h
	cw, ch := contentWidth(w), contentHeight(h)
	head := []string{m.tabHeader(), ""}

	if len(m.c.Blocks) == 0 {
		body := []string{center(grayStyle.Render("No blocks yet."), cw)}
		return renderPanel(stackContent(head, body, fixedHelp(blocksEmptyHelp, cw), ch), w, h, orange)
	}

	help := toggledHelp(blocksHelp, m.c.HelpFull, cw)
	if m.prompt != nil {
		help = []string{promptStyle.Render(m.prompt.prompt)}
	}

	rows := max(1, ch-len(head)-1-len(help)-1)
	lines := append([]string{}, head...)
	lines = append(lines, m.blockRows(cw, rows)...)

	out := make([]string, ch)
	copy(out, lines)
	placeHelp(out, help, len(head))
	return renderPanel(out, w, h, orange)
}

// the block table, windowed around the selection when it outgrows the panel
func (m model) blockRows(w, rows int) []string {
	n := len(m.c.Blocks)
	start := 0
	if n > rows {
		start = clamp(m.selBlock-rows/2, 0, n-rows)
	}
	end := min(n, start+rows)

	// columns are shed right to left as the panel narrows. days outranks the
	// source columns but yields to the title, which the list is unusable
	// without; at 34 columns there is not room for both.
	avail := w - 2 - colTime - colGap
	showDays := avail-colGap-colDays >= colMin
	rest := avail
	if showDays {
		rest = avail - colDays - colGap
	}
	titleW := max(1, min(maxTitleLen, rest))
	spare := rest - titleW
	typeW, valW := 0, 0
	if spare >= colGap+colMin {
		typeW = min(colType, spare-colGap)
		spare -= colGap + typeW
	}
	if typeW > 0 && spare >= colGap+colMin {
		valW = spare - colGap
	}

	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		b := m.c.Blocks[i]
		sel := i == m.selBlock
		mainStyle := textStyle
		marker := "  "
		if sel {
			mainStyle = orangeStyle.Bold(true)
			marker = "▶ "
		}

		span := fmtClockCompact(b.Start) + "-" + fmtClockCompact(b.End)
		row := marker + mainStyle.Render(padTo(span, colTime)) + spaces(colGap)
		if showDays {
			row += greenStyle.Render(padTo(explicitDays(b.ActiveDays), colDays)) + spaces(colGap)
		}
		row += mainStyle.Render(padTo(truncWidth(b.Title, titleW), titleW))

		if typeW > 0 {
			t, v := "-", "-"
			if b.Type != blkCycler {
				t, v = b.Source.label(), m.src[b.ID].text
			}
			row += spaces(colGap) + grayStyle.Render(padTo(truncWidth(t, typeW), typeW))
			if valW > 0 {
				row += spaces(colGap) + grayStyle.Render(truncWidth(v, valW))
			}
		}
		out = append(out, row)
	}
	return out
}

// always the per-day abbreviations, never collapsed, so the column lines up
func explicitDays(days []int) string {
	out := ""
	for _, d := range weekOrder {
		cell := "  "
		if activeOn(days, d) {
			cell = dayAbbrev[d]
		}
		if out != "" {
			out += " "
		}
		out += cell
	}
	return out
}

func padTo(s string, w int) string {
	return s + spaces(w-lipgloss.Width(s))
}
