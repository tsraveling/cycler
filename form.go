package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// @region form:model -- ADD/EDIT ITEM FORM

type field int

const (
	fName field = iota
	fMin
	fSec
	fType
	fSrc
	fButtons
	fieldCount
)

const formMaxW = 60

type formValues struct {
	name, mins, secs, src string
	typ                   int
}

type form struct {
	// empty when adding
	editID string

	name, mins, secs, src textinput.Model
	typ                   int

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
		name: newInput(maxNameLen),
		mins: newInput(3),
		secs: newInput(2),
		src:  newInput(0),
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
		f.src.SetValue(it.SourceConfig)
		for i, t := range sourceTypes {
			if t == it.Source {
				f.typ = i
			}
		}
	}
	f.orig = f.values()
	f.setFocus(fName)
	f.refreshPreview()
	return f
}

func (f *form) values() formValues {
	return formValues{f.name.Value(), f.mins.Value(), f.secs.Value(), f.src.Value(), f.typ}
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
	return map[field]*textinput.Model{fName: &f.name, fMin: &f.mins, fSec: &f.secs, fSrc: &f.src}
}

func (f *form) setFocus(to field) {
	f.focus = to
	for k, in := range f.inputs() {
		if k == to {
			in.Focus()
			in.CursorEnd()
		} else {
			in.Blur()
		}
	}
}

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
	return textinput.Blink
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
		f.setFocus((f.focus + 1) % fieldCount)
		return m, nil
	case "shift+tab":
		f.setFocus((f.focus + fieldCount - 1) % fieldCount)
		return m, nil
	case "esc":
		f.setFocus(fButtons)
		f.cancelSel = true
		m.cancelForm()
		return m, nil
	case "enter":
		if f.focus != fButtons {
			f.setFocus(f.focus + 1)
			return m, nil
		}
		if f.cancelSel {
			m.cancelForm()
		} else {
			m.saveForm()
		}
		return m, nil
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

	case fButtons:
		switch key {
		case "left", "h", "right", "l":
			f.cancelSel = !f.cancelSel
		case "?":
			m.toggleHelp()
		}
		return m, nil

	case fMin, fSec:
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
	if f.focus == fMin {
		limit = maxMinutes
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

func (m *model) saveForm() {
	f := m.f
	name := strings.TrimSpace(f.name.Value())
	if name == "" {
		f.err = "Name is required"
		return
	}
	secs, msg := f.duration()
	if msg != "" {
		f.err = msg
		return
	}

	it := item{
		ID:           f.editID,
		Name:         name,
		Seconds:      secs,
		Source:       sourceTypes[f.typ],
		SourceConfig: strings.TrimSpace(f.src.Value()),
	}
	wasEmpty := len(m.c.Items) == 0
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
	if wasEmpty {
		m.s.setCurrent(m.c, 0)
	}

	m.c.save()
	m.s.save()
	m.refreshSources()
	m.closeForm()
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

	lines := []string{
		label(fName, "Name"),
		f.name.View(),
		"",
		label(fMin, "Timer"),
		numView(&f.mins) + grayStyle.Render(":") + numView(&f.secs),
		"",
		label(fType, "Source type"),
		typStyle.Render(typ),
		"",
		label(fSrc, sourceTypes[f.typ].hint()),
		f.src.View(),
	}
	for _, l := range wrapCapped(f.preview.text, fw, 2) {
		lines = append(lines, f.preview.style().Render(l))
	}
	lines = append(lines, "", f.buttons())
	if f.err != "" {
		lines = append(lines, redStyle.Render(truncWidth(f.err, fw)))
	}

	// spacer rows go first when the panel is short
	if room := blockRows(len(head), len(help), ch); len(lines) > room {
		packed := lines[:0:0]
		for _, l := range lines {
			if l != "" {
				packed = append(packed, l)
			}
		}
		lines = packed
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
