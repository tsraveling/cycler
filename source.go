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

func empty(s string) sourceResult { return sourceResult{s, srcEmpty} }

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
		return sourceResult{cfg, srcOK}
	}
	if cfg == "" {
		return empty("Input a file")
	}
	path := expandPath(cfg)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return sourceResult{"File not found: " + cfg, srcErr}
		}
		return sourceResult{"Can't read " + cfg, srcErr}
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

var checkboxRe = regexp.MustCompile(`^\s*[-*+]\s+\[([ xX])\]\s*(.*)$`)

// checked, label and whether the line is a checkbox at all
func checkbox(line string) (bool, string, bool) {
	m := checkboxRe.FindStringSubmatch(line)
	if m == nil {
		return false, "", false
	}
	return m[1] != " ", strings.TrimSpace(m[2]), true
}

func isFence(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}

func parseMarkdownTodo(body string) sourceResult {
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		if isFence(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if checked, label, ok := checkbox(line); ok && !checked {
			return sourceResult{label, srcOK}
		}
	}
	return empty("No unchecked items.")
}

// reads `current` from YAML-ish front matter
func parseDrawdeck(body string) sourceResult {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return empty("No current item.")
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(k) != "current" {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if v == "" {
			break
		}
		return sourceResult{v, srcOK}
	}
	return empty("No current item.")
}

// trailing "(N)" duration suffix on a dispositio milestone title
var durationRe = regexp.MustCompile(`\s*\(\d+\)\s*$`)

type milestone struct {
	title    string
	finished bool
	open     string
	boxes    int
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
		if checked, label, ok := checkbox(line); ok {
			cur.boxes++
			if !checked && cur.open == "" {
				cur.open = label
			}
		}
	}

	if len(ms) == 0 {
		return empty("No milestones.")
	}
	for _, m := range ms {
		if m.finished {
			continue
		}
		task := m.open
		switch {
		case m.boxes == 0:
			task = "Plan"
		case task == "":
			task = "Review and complete"
		}
		return sourceResult{m.title + ": " + task, srcOK}
	}
	return empty("All milestones finished.")
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
