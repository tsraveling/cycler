package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// @region config:update -- ITEM LIST CONTROLS

// below this terminal width the detail pane is dropped
const detailMinCols = 60

func (m *model) openConfig() tea.Cmd {
	m.prompt = nil
	m.sel = max(0, m.c.indexOf(m.s.Current))
	m.scr = scrConfig
	return m.sourcesCmd()
}

func (m *model) leaveConfig() tea.Cmd {
	m.s.reconcile(m.c)
	m.s.save()
	m.scr = m.homeScreen()
	return m.sourcesCmd()
}

func (m model) updateConfig(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.prompt != nil {
		if m.prompt.handle(msg) == confirmYes {
			m.deleteSelected()
		}
		m.prompt = nil
		return m, nil
	}

	n := len(m.c.Items)
	switch msg.String() {
	case "a":
		return m, m.openForm(-1)
	case "esc":
		cmd := m.leaveConfig()
		return m, cmd
	case "?":
		m.toggleHelp()
		return m, nil
	}
	if n == 0 {
		return m, nil
	}

	switch msg.String() {
	case "e", "enter":
		return m, m.openForm(m.sel)
	case "up", "k":
		m.sel = max(0, m.sel-1)
	case "down", "j":
		m.sel = min(n-1, m.sel+1)
	case "shift+up", "K":
		m.moveSelected(-1)
	case "shift+down", "J":
		m.moveSelected(1)
	case "d":
		m.prompt = newConfirm(confirmDelete, "Delete? y/n")
	}
	return m, nil
}

func (m *model) moveSelected(dir int) {
	to := m.sel + dir
	if to < 0 || to >= len(m.c.Items) {
		return
	}
	m.c.Items[m.sel], m.c.Items[to] = m.c.Items[to], m.c.Items[m.sel]
	m.sel = to
	m.c.save()
}

func (m *model) deleteSelected() {
	i := m.sel
	id := m.c.Items[i].ID
	m.c.Items = append(m.c.Items[:i], m.c.Items[i+1:]...)
	if id == m.s.Current {
		m.s.handOff(m.c)
		m.carry = 0
	}
	m.s.reconcile(m.c)
	m.c.save()
	m.s.save()
	m.sel = clamp(m.sel, 0, max(0, len(m.c.Items)-1))
}

// @region config:view -- LIST + DETAIL PANE

func (m model) configView() string {
	w, h := m.w, m.h
	cw, ch := contentWidth(w), contentHeight(h)
	head := []string{orangeStyle.Bold(true).Render("Cycler Configuration"), ""}

	if len(m.c.Items) == 0 {
		body := []string{center(grayStyle.Render("No items yet."), cw)}
		return renderPanel(stackContent(head, body, fixedHelp(configEmptyHelp, cw), ch), w, h, orange)
	}

	help := toggledHelp(configHelp, m.c.HelpFull, cw)
	if m.prompt != nil {
		help = []string{promptStyle.Render(m.prompt.prompt)}
	}

	rows := max(1, ch-len(head)-1-len(help)-1)
	showDetail := w >= detailMinCols
	listW := cw
	if showDetail {
		listW = min(maxNameLen+4, cw/2)
	}

	left := m.listRows(listW, rows)
	var right []string
	if showDetail {
		right = m.detailRows(cw-listW-2, rows)
	}

	lines := append([]string{}, head...)
	for r := 0; r < max(len(left), len(right)); r++ {
		l := ""
		if r < len(left) {
			l = left[r]
		}
		if r < len(right) {
			l += spaces(listW-lipgloss.Width(l)+2) + right[r]
		}
		lines = append(lines, l)
	}

	out := make([]string, ch)
	copy(out, lines)
	placeHelp(out, help, len(head))
	return renderPanel(out, w, h, orange)
}

// the item list, windowed around the selection when it outgrows the panel
func (m model) listRows(w, rows int) []string {
	n := len(m.c.Items)
	start := 0
	if n > rows {
		start = clamp(m.sel-rows/2, 0, n-rows)
	}
	end := min(n, start+rows)

	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		it := m.c.Items[i]
		name := truncWidth(it.Name, w-3)
		switch {
		case i == m.sel:
			out = append(out, orangeStyle.Bold(true).Render("▶ "+name))
		case !m.s.eligible(it):
			out = append(out, grayStyle.Render("  "+name))
		default:
			out = append(out, textStyle.Render("  "+name))
		}
	}
	return out
}

func (m model) detailRows(w, rows int) []string {
	if w < 8 {
		return nil
	}
	it := m.c.Items[m.sel]
	const labelW = 8
	vw := max(1, w-labelW)
	row := func(label, val string, style lipgloss.Style) string {
		return grayStyle.Render(label+spaces(labelW-len(label))) + style.Render(truncWidth(val, vw))
	}

	limit, limitStyle := it.limitLabel(), grayStyle
	if it.TimesPerDay > 0 {
		limit = fmt.Sprintf("%s (%d done)", limit, m.s.countToday(it.ID))
		limitStyle = textStyle
	}
	daysStyle := textStyle
	if len(it.ActiveDays) == 0 {
		daysStyle = grayStyle
	}

	out := []string{
		row("Name", it.Name, boldStyle),
		row("Timer", fmtTime(it.Seconds), textStyle),
		row("Per day", limit, limitStyle),
		row("Active", it.daysLabel(), daysStyle),
		row("Source", it.Source.label(), textStyle),
	}
	r := m.src[it.ID]
	for _, l := range wrapCapped(r.text, vw, max(1, rows-len(out))) {
		out = append(out, spaces(labelW)+r.style().Render(l))
	}
	return out
}
