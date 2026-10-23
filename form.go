package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// @region form:model -- ADD/EDIT ITEM FORM

type field int

const (
	fName field = iota
	fMin
	fSec
	fTimes
	fDays
	fType
	fSrc
	fButtons
	fieldCount
)

// cursor positions in the active-on control: two presets, then Mo..Su
const (
	cellEveryDay = iota
	cellWeekdays
	cellFirstDay
)

const dayCellCount = cellFirstDay + 7

const formMaxW = 60

type formValues struct {
	name, mins, secs, times, days, src string
	typ                                int
}

type form struct {
	// empty when adding
	editID string

	name, mins, secs, times, src textinput.Model
	typ                          int
	days                         []int
	dayCur                       int

	focus     field
	cancelSel bool
	err       string
	preview   sourceResult

	orig formValues
}

func newInput(limit int) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = limit
	return ti
}

func newForm(it *item, w int) *form {
	f := &form{
		name:  newInput(maxNameLen),
		mins:  newInput(3),
		secs:  newInput(2),
		times: newInput(2),
		src:   newInput(0),
		days:  allDays(),
	}
	f.name.Placeholder = "Name"
	f.mins.Placeholder = "0"
	f.secs.Placeholder = "00"
	f.resize(w)
	if it != nil {
		f.editID = it.ID
		f.name.SetValue(it.Name)
		f.mins.SetValue(strconv.Itoa(it.Seconds / 60))
		f.secs.SetValue(fmt.Sprintf("%02d", it.Seconds%60))
		if it.TimesPerDay > 0 {
			f.times.SetValue(strconv.Itoa(it.TimesPerDay))
		}
		f.days = normalizeDays(it.ActiveDays)
		f.src.SetValue(it.SourceConfig)
		f.typ = max(0, slices.Index(sourceTypes, it.Source))
	}
	f.orig = f.values()
	f.refreshPreview()
	return f
}

func (f *form) values() formValues {
	return formValues{
		f.name.Value(), f.mins.Value(), f.secs.Value(),
		f.times.Value(), daysKey(f.days), f.src.Value(), f.typ,
	}
}

// the day set as a comparable value, so dirty tracking stays a struct compare
func daysKey(days []int) string {
	parts := make([]string, len(days))
	for i, d := range days {
		parts[i] = strconv.Itoa(d)
	}
	return strings.Join(parts, ",")
}

func (f *form) dirty() bool { return f.values() != f.orig }

func (f *form) width(termW int) int { return clamp(contentWidth(termW), 10, formMaxW) }

func (f *form) resize(termW int) {
	fw := f.width(termW)
	for _, in := range []*textinput.Model{&f.name, &f.src} {
		in.Width = fw - 1
		// the scroll window only recomputes on edit, so re-set to refit it
		pos := in.Position()
		in.SetValue(in.Value())
		in.SetCursor(pos)
	}
}

func (f *form) inputs() map[field]*textinput.Model {
	return map[field]*textinput.Model{
		fName: &f.name, fMin: &f.mins, fSec: &f.secs, fTimes: &f.times, fSrc: &f.src,
	}
}

func (f *form) setFocus(to field) tea.Cmd {
	f.focus = to
	var cmd tea.Cmd
	for k, in := range f.inputs() {
		if k == to {
			cmd = in.Focus()
			in.CursorEnd()
		} else {
			in.Blur()
		}
	}
	return cmd
}

type previewKey struct {
	typ sourceType
	cfg string
}

func (f *form) key() previewKey { return previewKey{sourceTypes[f.typ], f.src.Value()} }

func (f *form) refreshPreview() {
	f.preview = readSource(sourceTypes[f.typ], f.src.Value())
}

// routes non-key messages, like cursor blink, to the focused input
func (f *form) passThrough(msg tea.Msg) tea.Cmd {
	in, ok := f.inputs()[f.focus]
	if !ok {
		return nil
	}
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	return cmd
}

// total seconds, or an error message
func (f *form) duration() (int, string) {
	mins, _ := strconv.Atoi(f.mins.Value())
	secs, _ := strconv.Atoi(f.secs.Value())
	total := mins*60 + secs
	if total < 1 {
		return 0, "Timer must be longer than 0:00"
	}
	return total, ""
}

// @region form:update -- NAVIGATION, GATING, SAVE

func (m *model) openForm(idx int) tea.Cmd {
	var it *item
	if idx >= 0 && idx < len(m.c.Items) {
		it = &m.c.Items[idx]
	}
	m.f = newForm(it, m.w)
	m.prompt = nil
	m.scr = scrForm
	return m.f.setFocus(fName)
}

func (m *model) closeForm() {
	m.f = nil
	m.prompt = nil
	m.scr = scrConfig
}

func (m model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.f
	if m.prompt != nil {
		if m.prompt.handle(msg) == confirmYes {
			m.closeForm()
		}
		m.prompt = nil
		return m, nil
	}

	key := msg.String()
	switch key {
	case "tab":
		return m, f.setFocus((f.focus + 1) % fieldCount)
	case "shift+tab":
		return m, f.setFocus((f.focus + fieldCount - 1) % fieldCount)
	case "esc":
		f.setFocus(fButtons)
		f.cancelSel = true
		m.cancelForm()
		return m, nil
	case "enter":
		if f.focus != fButtons {
			return m, f.setFocus(f.focus + 1)
		}
		if f.cancelSel {
			m.cancelForm()
			return m, nil
		}
		return m, m.saveForm()
	}

	switch f.focus {
	case fType:
		switch key {
		case "left", "h":
			f.typ = (f.typ + len(sourceTypes) - 1) % len(sourceTypes)
			f.refreshPreview()
		case "right", "l":
			f.typ = (f.typ + 1) % len(sourceTypes)
			f.refreshPreview()
		case "?":
			m.toggleHelp()
		}
		return m, nil

	case fDays:
		switch key {
		case "left", "h":
			f.dayCur = max(0, f.dayCur-1)
		case "right", "l":
			f.dayCur = min(dayCellCount-1, f.dayCur+1)
		case " ":
			f.toggleDay()
		case "?":
			m.toggleHelp()
		}
		return m, nil

	case fButtons:
		switch key {
		case "left", "h", "right", "l":
			f.cancelSel = !f.cancelSel
		case "?":
			m.toggleHelp()
		}
		return m, nil

	case fMin, fSec, fTimes:
		return m, f.updateNumber(msg)
	}

	in := f.inputs()[f.focus]
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	if f.focus == fSrc {
		f.refreshPreview()
	}
	return m, cmd
}

// the presets only ever set days, so the control can never be emptied by
// accident; clearing every day is deliberate, one day at a time
func (f *form) toggleDay() {
	switch f.dayCur {
	case cellEveryDay:
		f.days = allDays()
	case cellWeekdays:
		f.days = weekdayDays()
	default:
		d := int(weekOrder[f.dayCur-cellFirstDay])
		if slices.Contains(f.days, d) {
			f.days = slices.DeleteFunc(slices.Clone(f.days), func(x int) bool { return x == d })
		} else {
			f.days = normalizeDays(append(slices.Clone(f.days), d))
		}
	}
}

// digits only, and the value must stay a valid time part
func (f *form) updateNumber(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyRunes {
		for _, r := range msg.Runes {
			if r < '0' || r > '9' {
				return nil
			}
		}
	}
	in := f.inputs()[f.focus]
	prev := in.Value()
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)

	limit := 59
	switch f.focus {
	case fMin:
		limit = maxMinutes
	case fTimes:
		limit = maxTimesPerDay
	}
	if v := in.Value(); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n > limit {
			in.SetValue(prev)
		}
	}
	return cmd
}

func (m *model) cancelForm() {
	if m.f.dirty() {
		m.prompt = newConfirm(confirmDiscard, "Discard?")
		return
	}
	m.closeForm()
}

func (m *model) saveForm() tea.Cmd {
	f := m.f
	name := strings.TrimSpace(f.name.Value())
	if name == "" {
		f.err = "Name is required"
		return nil
	}
	secs, msg := f.duration()
	if msg != "" {
		f.err = msg
		return nil
	}

	times, _ := strconv.Atoi(f.times.Value())
	it := item{
		ID:           f.editID,
		Name:         name,
		Seconds:      secs,
		TimesPerDay:  max(0, times),
		ActiveDays:   normalizeDays(f.days),
		Source:       sourceTypes[f.typ],
		SourceConfig: strings.TrimSpace(f.src.Value()),
	}
	if it.ID == "" {
		it.ID = newID()
		m.c.Items = append(m.c.Items, it)
		m.sel = len(m.c.Items) - 1
	} else if p := m.c.byID(it.ID); p != nil {
		*p = it
	}

	// a duration change resets an unstarted current item; a started one keeps
	// its remaining time under the new denominator
	if it.ID == m.s.Current {
		switch m.s.Timer {
		case timerIdle:
			m.s.Remaining = secs
		case timerGoing, timerPaused:
			m.s.Remaining = min(m.s.Remaining, secs)
		}
	}
	// an edit can schedule the running item out of today, and an added one can
	// fill a day that had nothing left to run
	if it.ID == m.s.Current && !m.s.eligible(it) {
		m.s.handOff(m.c)
		m.carry = 0
	}
	if m.s.Current == "" {
		m.s.setCurrent(m.c, m.s.firstEligible(m.c))
	}

	m.c.save()
	m.s.save()
	// supersede any read issued before the edit
	m.srcGen++
	m.src[it.ID] = f.preview
	m.closeForm()
	return nil
}

// @region form:view -- FIELD LAYOUT

func (m model) formView() string {
	w, h := m.w, m.h
	cw, ch := contentWidth(w), contentHeight(h)
	f := m.f
	fw := f.width(w)

	title := "New item"
	if f.editID != "" {
		title = "Edit item"
	}
	head := []string{orangeStyle.Bold(true).Render(title), ""}

	help := toggledHelp(formHelp, m.c.HelpFull, cw)
	if m.prompt != nil {
		help = []string{promptStyle.Render(m.prompt.prompt + " y/n")}
	}

	label := func(fl field, s string) string {
		if f.focus == fl || (fl == fMin && f.focus == fSec) {
			return orangeStyle.Render(s)
		}
		return grayStyle.Render(s)
	}

	typ := "◀ " + sourceTypes[f.typ].label() + " ▶"
	typStyle := textStyle
	if f.focus == fType {
		typStyle = orangeStyle.Bold(true)
	}

	build := func(withDaysValue, compact bool) []string {
		// a short panel puts a label and its value on one row, where they fit
		field := func(fl field, name, value string) []string {
			if one := label(fl, name) + "  " + value; compact && lipgloss.Width(one) <= fw {
				return []string{one}
			}
			return []string{label(fl, name), value}
		}
		lines := []string{label(fName, "Name"), f.name.View(), ""}
		lines = append(lines, field(fMin, "Timer", numView(&f.mins)+grayStyle.Render(":")+numView(&f.secs))...)
		lines = append(lines, "")
		lines = append(lines, field(fTimes, "Times per day", timesView(&f.times))...)
		lines = append(lines, "")
		rows := f.dayRows(fw)
		lines = append(lines, field(fDays, "Active on", rows[0])...)
		lines = append(lines, rows[1:]...)
		if withDaysValue {
			lines = append(lines, f.daysValue(fw))
		}
		lines = append(lines,
			"",
			label(fType, "Source type"),
			typStyle.Render(typ),
			"",
			label(fSrc, sourceTypes[f.typ].hint()),
			f.src.View(),
		)
		for _, l := range wrapCapped(f.preview.text, fw, 2) {
			lines = append(lines, f.preview.style().Render(l))
		}
		lines = append(lines, "", f.buttons())
		if f.err != "" {
			lines = append(lines, redStyle.Render(truncWidth(f.err, fw)))
		}
		return lines
	}

	// a short panel sheds the spacer rows first, then the day summary, which
	// only restates the row of toggles above it, then the row per label
	room := blockRows(len(head), len(help), ch)
	lines := build(true, false)
	if len(lines) > room {
		lines = dropBlanks(lines)
	}
	if len(lines) > room {
		lines = dropBlanks(build(false, false))
	}
	if len(lines) > room {
		lines = dropBlanks(build(false, true))
	}

	block := padBlock(lines, fw, cw)
	return renderPanel(stackContent(head, block, help, ch), w, h, orange)
}

func (f *form) buttons() string {
	saveStyle, cancelStyle := greenStyle, grayStyle
	if f.cancelSel {
		saveStyle, cancelStyle = grayStyle, redStyle
	}
	if f.focus == fButtons {
		if f.cancelSel {
			cancelStyle = cancelStyle.Bold(true)
		} else {
			saveStyle = saveStyle.Bold(true)
		}
	}
	return saveStyle.Render("[ save ]") + "   " + cancelStyle.Render("[ cancel ]")
}

func dropBlanks(lines []string) []string {
	packed := lines[:0:0]
	for _, l := range lines {
		if l != "" {
			packed = append(packed, l)
		}
	}
	return packed
}

// the preset cells and the seven day cells, on one row when it fits
func (f *form) dayRows(fw int) []string {
	cell := func(i int, text string, on bool) string {
		switch {
		case f.focus == fDays && f.dayCur == i:
			return orangeStyle.Bold(true).Render(text)
		case on:
			return textStyle.Render(text)
		}
		return grayStyle.Render(text)
	}
	presets := cell(cellEveryDay, "Every day", slices.Equal(f.days, allDays())) +
		"  " + cell(cellWeekdays, "Weekdays", slices.Equal(f.days, weekdayDays()))
	cells := make([]string, len(weekOrder))
	for i, d := range weekOrder {
		cells[i] = cell(cellFirstDay+i, dayAbbrev[d], slices.Contains(f.days, int(d)))
	}
	days := strings.Join(cells, " ")
	if one := presets + "  " + days; lipgloss.Width(one) <= fw {
		return []string{one}
	}
	return []string{presets, days}
}

// the day set as it will read back on the config screen
func (f *form) daysValue(fw int) string {
	style := textStyle
	if len(f.days) == 0 {
		style = grayStyle
	}
	return style.Render(truncWidth(item{ActiveDays: f.days}.daysLabel(), fw))
}

// an empty limit reads as "no limit" once the field is left
func timesView(in *textinput.Model) string {
	if in.Focused() {
		return in.View()
	}
	if in.Value() == "" {
		return grayStyle.Render("no limit")
	}
	return textStyle.Render(in.Value())
}

// a blurred input still reserves a cursor cell, which would gap the colon
func numView(in *textinput.Model) string {
	if in.Focused() {
		return in.View()
	}
	if in.Value() == "" {
		return grayStyle.Render(in.Placeholder)
	}
	return textStyle.Render(in.Value())
}

// left-aligns rows in a block of width bw, centered in cw
func padBlock(rows []string, bw, cw int) []string {
	left := spaces(max(0, (cw-bw)/2))
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = left + r
	}
	return out
}
