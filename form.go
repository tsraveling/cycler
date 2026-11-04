package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// @region form:model -- ADD/EDIT FORM, SHARED BY ITEMS AND BLOCKS

type field int

const (
	fName field = iota
	fTimer
	fTimes
	fTitle
	fType
	fColor
	fStart
	fEnd
	fDays
	fSrcType
	fSrc
	fButtons
)

type formKind int

const (
	formItem formKind = iota
	formBlock
)

// cursor positions in the active-on control: two presets, then Mo..Su
const (
	cellEveryDay = iota
	cellWeekdays
	cellFirstDay
)

const dayCellCount = cellFirstDay + 7

const formMaxW = 60

// one navigable row. a text line can be entered to edit; everything else acts
// directly when selected.
type formLine struct {
	f        field
	text     bool
	cells    int
	disabled bool
}

// every mutable field, flattened so dirty tracking stays a struct compare
type formValues struct {
	name, mins, secs, times, days, src string
	title, sh, sm, eh, em              string
	typ, btyp, color                   int
}

type form struct {
	kind formKind
	// empty when adding
	editID string

	// text inputs per field; a two-cell field holds two
	ins map[field][]*textinput.Model

	typ   int
	btyp  int
	color int

	days   []int
	dayCur int

	focus field
	// cursor is inside the focused line's input
	editing bool
	// which cell of a two-cell line has the cursor
	cell int

	cancelSel bool
	err       string
	preview   sourceResult

	orig formValues
}

func newInput(limit int) *textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = limit
	return &ti
}

func (f *form) base() {
	f.ins = map[field][]*textinput.Model{}
	f.days = allDays()
}

func newItemForm(it *item, w int) *form {
	f := &form{kind: formItem, focus: fName}
	f.base()
	name, mins, secs, times := newInput(maxNameLen), newInput(3), newInput(2), newInput(2)
	src := newInput(0)
	name.Placeholder = "Name"
	mins.Placeholder = "0"
	secs.Placeholder = "00"
	f.ins[fName] = []*textinput.Model{name}
	f.ins[fTimer] = []*textinput.Model{mins, secs}
	f.ins[fTimes] = []*textinput.Model{times}
	f.ins[fSrc] = []*textinput.Model{src}

	if it != nil {
		f.editID = it.ID
		name.SetValue(it.Name)
		mins.SetValue(strconv.Itoa(it.Seconds / 60))
		secs.SetValue(fmt.Sprintf("%02d", it.Seconds%60))
		if it.TimesPerDay > 0 {
			times.SetValue(strconv.Itoa(it.TimesPerDay))
		}
		f.days = normalizeDays(it.ActiveDays)
		src.SetValue(it.SourceConfig)
		f.typ = max(0, slices.Index(sourceTypes, it.Source))
	}
	f.finish(it == nil, w)
	return f
}

// prefillStart and prefillDays carry the `o` insert-after hint; prefillStart is
// -1 when adding plain
func newBlockForm(b *block, prefillStart int, prefillDays []int, w int) *form {
	f := &form{kind: formBlock, focus: fTitle, color: blockColorIndex(defaultBlockColor)}
	f.base()
	f.days = weekdayDays()
	title, src := newInput(maxTitleLen), newInput(0)
	title.Placeholder = "Title"
	f.ins[fTitle] = []*textinput.Model{title}
	f.ins[fStart] = []*textinput.Model{newInput(2), newInput(2)}
	f.ins[fEnd] = []*textinput.Model{newInput(2), newInput(2)}
	f.ins[fSrc] = []*textinput.Model{src}

	if b != nil {
		f.editID = b.ID
		title.SetValue(b.Title)
		f.setClock(fStart, b.Start)
		f.setClock(fEnd, b.End)
		f.days = normalizeDays(b.ActiveDays)
		f.btyp = max(0, slices.Index(blockTypes, b.Type))
		f.color = blockColorIndex(b.Color)
		src.SetValue(b.SourceConfig)
		f.typ = max(0, slices.Index(sourceTypes, b.Source))
	} else {
		if prefillStart >= 0 {
			f.setClock(fStart, prefillStart)
		}
		if len(prefillDays) > 0 {
			f.days = normalizeDays(prefillDays)
		}
	}
	f.finish(b == nil, w)
	return f
}

// shared tail of both constructors: size, snapshot, preview, opening focus
func (f *form) finish(isNew bool, w int) {
	f.resize(w)
	f.orig = f.values()
	f.refreshPreview()
	if isNew {
		f.beginEdit()
	}
}

func (f *form) setClock(fl field, min int) {
	f.ins[fl][0].SetValue(fmt.Sprintf("%02d", (min/60)%24))
	f.ins[fl][1].SetValue(fmt.Sprintf("%02d", min%60))
}

func (f *form) val(fl field, cell int) string {
	in, ok := f.ins[fl]
	if !ok || cell >= len(in) {
		return ""
	}
	return in[cell].Value()
}

func (f *form) values() formValues {
	return formValues{
		name: f.val(fName, 0), mins: f.val(fTimer, 0), secs: f.val(fTimer, 1),
		times: timesKey(f.val(fTimes, 0)), days: daysKey(f.days), src: f.val(fSrc, 0),
		title: f.val(fTitle, 0),
		sh:    f.val(fStart, 0), sm: f.val(fStart, 1),
		eh: f.val(fEnd, 0), em: f.val(fEnd, 1),
		typ: f.typ, btyp: f.btyp, color: f.color,
	}
}

// every spelling of no limit compares equal, so typing a bare 0 into an
// unlimited item is not an edit
func timesKey(v string) string {
	n, err := strconv.Atoi(v)
	if err != nil || n == 0 {
		return ""
	}
	return strconv.Itoa(n)
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

func (f *form) isCycler() bool { return f.kind == formBlock && blockTypes[f.btyp] == blkCycler }

// the navigable rows, in order, for this form's kind
func (f *form) lines() []formLine {
	if f.kind == formBlock {
		off := f.isCycler()
		return []formLine{
			{f: fTitle, text: true, cells: 1},
			{f: fType, cells: 1},
			{f: fColor, cells: 1, disabled: off},
			{f: fStart, text: true, cells: 2},
			{f: fEnd, text: true, cells: 2},
			{f: fDays, cells: 1},
			{f: fSrcType, cells: 1, disabled: off},
			{f: fSrc, text: true, cells: 1, disabled: off},
			{f: fButtons, cells: 1},
		}
	}
	return []formLine{
		{f: fName, text: true, cells: 1},
		{f: fTimer, text: true, cells: 2},
		{f: fTimes, text: true, cells: 1},
		{f: fDays, cells: 1},
		{f: fSrcType, cells: 1},
		{f: fSrc, text: true, cells: 1},
		{f: fButtons, cells: 1},
	}
}

func (f *form) line() formLine {
	for _, l := range f.lines() {
		if l.f == f.focus {
			return l
		}
	}
	return formLine{f: f.focus, cells: 1}
}

func (f *form) lineIdx() int {
	for i, l := range f.lines() {
		if l.f == f.focus {
			return i
		}
	}
	return 0
}

// @region form:nav -- SELECT / EDIT MODES

// moves the selection by dir, skipping disabled lines and leaving edit mode
func (f *form) move(dir int) {
	ls := f.lines()
	i := f.lineIdx()
	for n := 0; n < len(ls); n++ {
		i = (i + dir + len(ls)) % len(ls)
		if !ls[i].disabled {
			break
		}
	}
	f.commit()
	f.focus = ls[i].f
	f.cell = 0
}

// moves by dir and starts editing when the landing line is text
func (f *form) moveEdit(dir int) {
	f.move(dir)
	if f.line().text {
		f.beginEdit()
		if dir < 0 {
			f.cell = f.line().cells - 1
		}
		f.focusCell()
	}
}

func (f *form) beginEdit() {
	if !f.line().text {
		return
	}
	f.editing = true
	f.focusCell()
}

// leaves edit mode, validating whatever the line needs validated
func (f *form) commit() {
	if !f.editing {
		return
	}
	f.editing = false
	f.blurAll()
	// a limit of zero is no limit, which reads as an empty field
	if n, err := strconv.Atoi(f.val(fTimes, 0)); err == nil && n == 0 {
		f.ins[fTimes][0].SetValue("")
	}
}

func (f *form) blurAll() {
	for _, ins := range f.ins {
		for _, in := range ins {
			in.Blur()
		}
	}
}

func (f *form) focusCell() tea.Cmd {
	f.blurAll()
	ins, ok := f.ins[f.focus]
	if !ok || f.cell >= len(ins) {
		return nil
	}
	cmd := ins[f.cell].Focus()
	ins[f.cell].CursorEnd()
	return cmd
}

// the input the cursor is in, or nil
func (f *form) active() *textinput.Model {
	if !f.editing {
		return nil
	}
	ins, ok := f.ins[f.focus]
	if !ok || f.cell >= len(ins) {
		return nil
	}
	return ins[f.cell]
}

type previewKey struct {
	typ sourceType
	cfg string
}

func (f *form) key() previewKey { return previewKey{sourceTypes[f.typ], f.val(fSrc, 0)} }

func (f *form) refreshPreview() {
	f.preview = readSource(sourceTypes[f.typ], f.val(fSrc, 0))
}

// routes non-key messages, like cursor blink, to the input being edited
func (f *form) passThrough(msg tea.Msg) tea.Cmd {
	in := f.active()
	if in == nil {
		return nil
	}
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	return cmd
}

func (f *form) resize(termW int) {
	fw := f.width(termW)
	for _, fl := range []field{fName, fTitle, fSrc} {
		ins, ok := f.ins[fl]
		if !ok {
			continue
		}
		in := ins[0]
		in.Width = fw - 1
		// the scroll window only recomputes on edit, so re-set to refit it
		pos := in.Position()
		in.SetValue(in.Value())
		in.SetCursor(pos)
	}
}

func (f *form) width(termW int) int { return clamp(contentWidth(termW), 10, formMaxW) }

// total seconds, or an error message
func (f *form) duration() (int, string) {
	mins, _ := strconv.Atoi(f.val(fTimer, 0))
	secs, _ := strconv.Atoi(f.val(fTimer, 1))
	total := mins*60 + secs
	if total < 1 {
		return 0, "Timer must be longer than 0:00"
	}
	return total, ""
}

// minutes from midnight for a time line, and whether it reads as a valid time
func (f *form) clock(fl field) (int, bool) {
	return parseClock(f.val(fl, 0), f.val(fl, 1))
}

func (f *form) clockValid(fl field) bool {
	_, ok := f.clock(fl)
	return ok
}

// save is inert while a time field cannot be read
func (f *form) saveReady() bool {
	if f.kind != formBlock {
		return true
	}
	return f.clockValid(fStart) && f.clockValid(fEnd)
}

// @region form:update -- KEY ROUTING

func (m *model) openForm(idx int) tea.Cmd {
	var it *item
	if idx >= 0 && idx < len(m.c.Items) {
		it = &m.c.Items[idx]
	}
	m.f = newItemForm(it, m.w)
	m.prompt = nil
	m.scr = scrForm
	return m.f.focusCell()
}

// idx < 0 adds; prefill carries `o` insert-after, with -1 meaning none
func (m *model) openBlockForm(idx, prefillStart int, prefillDays []int) tea.Cmd {
	var b *block
	if idx >= 0 && idx < len(m.c.Blocks) {
		b = &m.c.Blocks[idx]
	}
	m.f = newBlockForm(b, prefillStart, prefillDays, m.w)
	m.prompt = nil
	m.scr = scrForm
	return m.f.focusCell()
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
	if f.editing {
		return m.updateEditing(msg)
	}
	return m.updateSelected(msg)
}

// the cursor is inside an input: almost every key is literal text
func (m model) updateEditing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.f
	cells := f.line().cells
	switch msg.String() {
	case "enter":
		// walks the cells, then commits the whole line
		if f.cell < cells-1 {
			f.cell++
			return m, f.focusCell()
		}
		f.commit()
		return m, nil
	case "esc":
		// commits rather than reverting, and does not cancel the form
		f.commit()
		return m, nil
	case "tab":
		if f.cell < cells-1 {
			f.cell++
			return m, f.focusCell()
		}
		f.moveEdit(1)
		return m, f.focusCell()
	case "shift+tab":
		if f.cell > 0 {
			f.cell--
			return m, f.focusCell()
		}
		f.moveEdit(-1)
		return m, f.focusCell()
	case "up":
		f.move(-1)
		return m, nil
	case "down":
		f.move(1)
		return m, nil
	}

	if f.numeric() {
		return m, f.updateNumber(msg)
	}
	in := f.active()
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	if f.focus == fSrc {
		f.refreshPreview()
	}
	return m, cmd
}

// the line is selected but not being edited: keys act on the control directly
func (m model) updateSelected(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.f
	switch msg.String() {
	case "esc":
		f.cancelSel = true
		f.focus = fButtons
		m.cancelForm()
		return m, nil
	case "?":
		m.toggleHelp()
		return m, nil
	case "up", "k":
		f.move(-1)
		return m, nil
	case "down", "j":
		f.move(1)
		return m, nil
	case "shift+tab":
		f.move(-1)
		return m, nil
	case "tab":
		f.move(1)
		return m, nil
	case "enter":
		if f.focus == fButtons {
			if f.cancelSel {
				m.cancelForm()
				return m, nil
			}
			return m, m.saveForm()
		}
		f.beginEdit()
		return m, f.focusCell()
	}

	key := msg.String()
	left := key == "left" || key == "h"
	right := key == "right" || key == "l"

	switch f.focus {
	case fButtons:
		if left || right {
			f.cancelSel = !f.cancelSel
		}
	case fSrcType:
		switch {
		case left:
			f.typ = (f.typ + len(sourceTypes) - 1) % len(sourceTypes)
			f.refreshPreview()
		case right:
			f.typ = (f.typ + 1) % len(sourceTypes)
			f.refreshPreview()
		}
	case fType:
		switch {
		case left:
			f.btyp = (f.btyp + len(blockTypes) - 1) % len(blockTypes)
		case right:
			f.btyp = (f.btyp + 1) % len(blockTypes)
		}
		// switching to cycler disables the line the cursor may be standing on
		if f.line().disabled {
			f.move(1)
		}
	case fColor:
		switch {
		case left:
			f.color = (f.color + len(blockColors) - 1) % len(blockColors)
		case right:
			f.color = (f.color + 1) % len(blockColors)
		}
	case fDays:
		switch {
		case left:
			f.dayCur = max(0, f.dayCur-1)
		case right:
			f.dayCur = min(dayCellCount-1, f.dayCur+1)
		case key == " ":
			f.toggleDay()
		}
	}
	return m, nil
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

// lines whose input only accepts digits
func (f *form) numeric() bool {
	switch f.focus {
	case fTimer, fTimes, fStart, fEnd:
		return true
	}
	return false
}

// digits only. the item timer also clamps to a valid time part; block times do
// not, so an out-of-range entry survives to be reported as invalid.
func (f *form) updateNumber(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyRunes {
		for _, r := range msg.Runes {
			if r < '0' || r > '9' {
				return nil
			}
		}
	}
	in := f.active()
	prev := in.Value()
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)

	limit := -1
	switch f.focus {
	case fTimer:
		limit = 59
		if f.cell == 0 {
			limit = maxMinutes
		}
	case fTimes:
		limit = maxTimesPerDay
	}
	if v := in.Value(); limit >= 0 && v != "" {
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
	if m.f.kind == formBlock {
		return m.saveBlockForm()
	}
	return m.saveItemForm()
}

func (m *model) saveItemForm() tea.Cmd {
	f := m.f
	name := strings.TrimSpace(f.val(fName, 0))
	if name == "" {
		f.err = "Name is required"
		return nil
	}
	secs, msg := f.duration()
	if msg != "" {
		f.err = msg
		return nil
	}

	times, _ := strconv.Atoi(f.val(fTimes, 0))
	it := item{
		ID:           f.editID,
		Name:         name,
		Seconds:      secs,
		TimesPerDay:  max(0, times),
		ActiveDays:   normalizeDays(f.days),
		Source:       sourceTypes[f.typ],
		SourceConfig: strings.TrimSpace(f.val(fSrc, 0)),
	}
	if it.ID == "" {
		it.ID = newID()
		at := clamp(m.insertAt, 0, len(m.c.Items))
		m.c.Items = slices.Insert(m.c.Items, at, it)
		m.sel = at
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

func (m *model) saveBlockForm() tea.Cmd {
	f := m.f
	if !f.saveReady() {
		return nil
	}
	title := strings.TrimSpace(f.val(fTitle, 0))
	if title == "" {
		f.err = "Title is required"
		return nil
	}
	start, _ := f.clock(fStart)
	end, _ := f.clock(fEnd)
	if end <= start {
		f.err = "End must be after start"
		return nil
	}

	b := block{
		ID:           f.editID,
		Title:        title,
		Type:         blockTypes[f.btyp],
		Color:        blockColors[f.color].key,
		Start:        start,
		End:          end,
		ActiveDays:   normalizeDays(f.days),
		Source:       sourceTypes[f.typ],
		SourceConfig: strings.TrimSpace(f.val(fSrc, 0)),
	}
	if o := m.c.overlaps(b); o != nil {
		f.err = "Overlaps " + o.Title
		return nil
	}

	if b.ID == "" {
		b.ID = newID()
		m.c.Blocks = append(m.c.Blocks, b)
	} else {
		if p := m.c.blockByID(b.ID); p != nil {
			// a window that moved makes the day's drift from it meaningless
			if p.Start != b.Start || p.End != b.End {
				m.s.dropBlockOverride(b.ID)
			}
			*p = b
		}
	}
	m.c.sortBlocks()
	m.selBlock = max(0, m.c.blockIndexOf(b.ID))

	m.c.save()
	m.s.save()
	m.srcGen++
	m.src[b.ID] = f.preview
	m.closeForm()
	return nil
}
