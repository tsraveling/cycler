package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// @region app:source -- CURRENT TASK FROM EXTERNAL FILES

type sourceType string

const (
	srcMarkdown   sourceType = "markdown"
	srcDrawdeck   sourceType = "drawdeck"
	srcDispositio sourceType = "dispositio"
	srcStatic     sourceType = "static"
)

var sourceTypes = []sourceType{srcMarkdown, srcDrawdeck, srcDispositio, srcStatic}

func (t sourceType) valid() bool { return slices.Contains(sourceTypes, t) }

func (t sourceType) label() string {
	switch t {
	case srcMarkdown:
		return "Markdown TODO"
	case srcDrawdeck:
		return "Drawdeck"
	case srcDispositio:
		return "Dispositio"
	}
	return "Static"
}

func (t sourceType) hint() string {
	if t == srcStatic {
		return "Text to display"
	}
	return "Path to markdown / deck / Dispositio plan"
}

type sourceKind int

const (
	srcOK sourceKind = iota
	srcEmpty
	srcErr
)

type sourceResult struct {
	text string
	kind sourceKind
	// the line under the main one; empty for none
	sub string
	// the sub line is a warning, not a status
	subWarn bool
}

func (r sourceResult) style() lipgloss.Style {
	switch r.kind {
	case srcEmpty:
		return grayStyle
	case srcErr:
		return redStyle
	}
	return srcStyle
}

func (r sourceResult) subStyle() lipgloss.Style {
	if r.subWarn {
		return redStyle
	}
	return grayStyle
}

// the main line and its subline, ready for the panel
func (r sourceResult) line() statusLine {
	return statusLine{text: r.text, sub: r.sub, style: r.style(), subStyle: r.subStyle()}
}

func empty(s string) sourceResult { return sourceResult{text: s, kind: srcEmpty} }

// overridable so tests can resolve relative paths against a fixture dir
var homeDirFn = os.UserHomeDir

// expands ~ and resolves relative paths against $HOME
func expandPath(p string) string {
	home, err := homeDirFn()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) {
		return filepath.Join(home, p)
	}
	return p
}

func readSource(t sourceType, cfg string) sourceResult {
	cfg = strings.TrimSpace(cfg)
	if t == srcStatic {
		if cfg == "" {
			return empty("Input text")
		}
		return sourceResult{text: cfg, kind: srcOK}
	}
	if cfg == "" {
		return empty("Input a file")
	}
	path := expandPath(cfg)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return sourceResult{text: "File not found: " + cfg, kind: srcErr}
		}
		return sourceResult{text: "Can't read " + cfg, kind: srcErr}
	}
	body := string(data)
	switch t {
	case srcMarkdown:
		return parseMarkdownTodo(body)
	case srcDrawdeck:
		return parseDrawdeck(body)
	case srcDispositio:
		return parseDispositio(body)
	}
	return empty("Unknown source")
}

var checkboxRe = regexp.MustCompile(`^(\s*)[-*+]\s+\[([ xX])\]\s*(.*)$`)

// indented quote line under a drawdeck card, any depth
var noteRe = regexp.MustCompile(`^\s+>\s?(.*)$`)

// indent, checked, label and whether the line is a checkbox at all
func checkboxAt(line string) (int, bool, string, bool) {
	m := checkboxRe.FindStringSubmatch(line)
	if m == nil {
		return 0, false, "", false
	}
	return indentWidth(m[1]), m[2] != " ", strings.TrimSpace(m[3]), true
}

// leading whitespace in columns; a tab counts as four
func indentWidth(s string) int {
	n := 0
	for _, r := range s {
		if r == '\t' {
			n += 4
			continue
		}
		n++
	}
	return n
}

func isFence(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}

// @region source:subtask -- THE LINE UNDER THE MAIN STATUS

const (
	reviewStatus = "Review and complete"
	planStatus   = "Plan and begin"
)

// the first unchecked direct child of the box above start, "" when every child
// is checked, and whether the box has any children at all. direct children are
// the boxes at the indent of the first one deeper than the parent; the run ends
// at a box indented no further than the parent, or at a heading.
func childStatus(lines []string, start, parentIndent int) (string, bool) {
	childIndent := -1
	open, has, inFence := "", false, false
	for _, line := range lines[min(start, len(lines)):] {
		if isFence(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			break
		}
		ind, checked, label, ok := checkboxAt(line)
		if !ok {
			continue
		}
		if ind <= parentIndent {
			break
		}
		if childIndent < 0 {
			childIndent = ind
		}
		if ind != childIndent {
			continue
		}
		has = true
		if !checked && open == "" {
			open = label
		}
	}
	return open, has
}

// @region source:parse -- THE THREE FILE FORMATS

func parseMarkdownTodo(body string) sourceResult {
	lines := strings.Split(body, "\n")
	inFence := false
	for i, line := range lines {
		if isFence(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		ind, checked, label, ok := checkboxAt(line)
		if !ok || checked {
			continue
		}
		sub, has := childStatus(lines, i+1, ind)
		if has && sub == "" {
			sub = reviewStatus
		}
		return sourceResult{text: label, kind: srcOK, sub: sub}
	}
	return empty("No unchecked items.")
}

// reads `current` from YAML-ish front matter, then the card's own notes
func parseDrawdeck(body string) sourceResult {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return empty("No current item.")
	}
	cur, end := "", len(lines)
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i + 1
			break
		}
		k, v, ok := strings.Cut(lines[i], ":")
		if !ok || strings.TrimSpace(k) != "current" || cur != "" {
			continue
		}
		cur = unquote(strings.TrimSpace(v))
	}
	if cur == "" {
		return empty("No current item.")
	}
	notes, found := deckNotes(lines[min(end, len(lines)):], cur)
	if !found {
		return sourceResult{text: cur, kind: srcOK, sub: "Warning: card no longer exists.", subWarn: true}
	}
	return sourceResult{text: cur, kind: srcOK, sub: notes}
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// the joined quote lines under the card titled want, and whether that card is
// still in the deck at all
func deckNotes(lines []string, want string) (string, bool) {
	for i, line := range lines {
		ind, _, label, ok := checkboxAt(line)
		if !ok || ind != 0 || label != want {
			continue
		}
		var parts []string
		for _, l := range lines[i+1:] {
			m := noteRe.FindStringSubmatch(l)
			if strings.TrimSpace(l) == "" || m == nil {
				break
			}
			parts = append(parts, strings.TrimSpace(m[1]))
		}
		return strings.Join(parts, " "), true
	}
	return "", false
}

// trailing "(N)" duration suffix on a dispositio milestone title
var durationRe = regexp.MustCompile(`\s*\(\d+\)\s*$`)

// a milestone's top-level task; its children are read from the file on demand
type dispTask struct {
	title   string
	checked bool
	line    int
}

type milestone struct {
	title    string
	finished bool
	tasks    []dispTask
}

func parseDispositio(body string) sourceResult {
	lines := strings.Split(body, "\n")
	var ms []*milestone
	var cur *milestone
	inFence := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if isFence(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			cur = &milestone{title: durationRe.ReplaceAllString(strings.TrimSpace(line[2:]), "")}
			ms = append(ms, cur)
			i = scanMeta(lines, i+1, cur) - 1
			continue
		}
		if cur == nil {
			continue
		}
		if ind, checked, label, ok := checkboxAt(line); ok && ind == 0 {
			cur.tasks = append(cur.tasks, dispTask{label, checked, i})
		}
	}

	if len(ms) == 0 {
		return empty("No milestones.")
	}
	for _, m := range ms {
		if m.finished {
			continue
		}
		return sourceResult{text: m.title, kind: srcOK, sub: m.subStatus(lines)}
	}
	return empty("All milestones finished.")
}

// the task line under the milestone title
func (m *milestone) subStatus(lines []string) string {
	if len(m.tasks) == 0 {
		return planStatus
	}
	for _, t := range m.tasks {
		if t.checked {
			continue
		}
		kid, has := childStatus(lines, t.line+1, 0)
		switch {
		case !has:
			return t.title
		case kid == "":
			return t.title + " > " + reviewStatus
		}
		return t.title + " > " + kid
	}
	return reviewStatus
}

// reads the metadata code block directly under a heading, if any, marking the
// milestone finished. returns the index of the first line after the block.
func scanMeta(lines []string, i int, m *milestone) int {
	j := i
	for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
		j++
	}
	if j >= len(lines) || !isFence(lines[j]) {
		return i
	}
	for j++; j < len(lines); j++ {
		if isFence(lines[j]) {
			return j + 1
		}
		if k, _, ok := strings.Cut(lines[j], ":"); ok && strings.TrimSpace(k) == "Finished" {
			m.finished = true
		}
	}
	return j
}
