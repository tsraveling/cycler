package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkdownTodo(t *testing.T) {
	cases := []struct {
		name, body, want, sub string
		kind                  sourceKind
	}{
		{"first unchecked", "# T\n- [x] done\n- [ ] next one\n- [ ] later", "next one", "", srcOK},
		{"nested and bullets", "* [x] a\n    + [ ] deep", "deep", "", srcOK},
		{"fenced ignored", "```\n- [ ] fake\n```\n- [ ] real", "real", "", srcOK},
		{"capital X is checked", "- [X] a\n- [ ] b", "b", "", srcOK},
		{"none", "- [x] a\n- [x] b", "No unchecked items.", "", srcEmpty},
		{"first open child", "- [ ] p\n  - [x] one\n  - [ ] two", "p", "two", srcOK},
		{"children all done", "- [ ] p\n  - [x] one\n  - [x] two", "p", reviewStatus, srcOK},
		{"grandchildren ignored", "- [ ] p\n  - [x] one\n    - [ ] deep\n  - [ ] two", "p", "two", srcOK},
		{"run ends at sibling", "- [ ] p\n- [ ] q\n  - [ ] kid", "p", "", srcOK},
		{"run ends at heading", "- [ ] p\n# H\n  - [ ] kid", "p", "", srcOK},
		{"prose does not end the run", "- [ ] p\n\nsome note\n\n  - [ ] kid", "p", "kid", srcOK},
		{"fenced child ignored", "- [ ] p\n  ```\n  - [ ] fake\n  ```\n  - [ ] kid", "p", "kid", srcOK},
		{"tabs indent", "- [ ] p\n\t- [ ] kid", "p", "kid", srcOK},
	}
	for _, c := range cases {
		r := parseMarkdownTodo(c.body)
		if r.text != c.want || r.sub != c.sub || r.kind != c.kind {
			t.Errorf("%s: got %q/%q/%d", c.name, r.text, r.sub, r.kind)
		}
	}
}

const deckGone = "Warning: card no longer exists."

func TestDrawdeck(t *testing.T) {
	deck := func(body string) string { return "---\ncurrent: \"A woolly yak\"\n---\n# Deck\n" + body }
	cases := []struct {
		body, want, sub string
		kind            sourceKind
		warn            bool
	}{
		{"---\ncurrent: \"CHL Long Piece\"\n---\n# Focus", "CHL Long Piece", deckGone, srcOK, true},
		{"---\ntitle: x\ncurrent: 'single'\n---", "single", deckGone, srcOK, true},
		{"---\ncurrent: bare value\n---", "bare value", deckGone, srcOK, true},
		{"---\ntitle: x\n---\ncurrent: outside", "No current item.", "", srcEmpty, false},
		{"---\ncurrent: \"\"\n---", "No current item.", "", srcEmpty, false},
		{"# no front matter", "No current item.", "", srcEmpty, false},
		{deck("- [x] A woolly yak\n    > the note"), "A woolly yak", "the note", srcOK, false},
		{deck("- [ ] A woolly yak"), "A woolly yak", "", srcOK, false},
		{deck("- [x] A woolly yak\n    > one\n    > two\n\n- [ ] other"), "A woolly yak", "one two", srcOK, false},
		{deck("- [x] Another card\n    > not mine\n- [x] A woolly yak"), "A woolly yak", "", srcOK, false},
		{deck("  - [x] A woolly yak\n    > indented card"), "A woolly yak", deckGone, srcOK, true},
	}
	for _, c := range cases {
		r := parseDrawdeck(c.body)
		if r.text != c.want || r.sub != c.sub || r.kind != c.kind || r.subWarn != c.warn {
			t.Errorf("%q: got %q/%q/%d/%v", c.body, r.text, r.sub, r.kind, r.subWarn)
		}
	}
}

func TestDispositio(t *testing.T) {
	head := "```\nProject Name: X\n```\n\n"
	done := "# Old (2)\n```\nFinished: Apr 2 2026\n```\n\n- [ ] stray\n\n"
	cases := []struct {
		name, body, want, sub string
		kind                  sourceKind
	}{
		{"open task, duration stripped", head + done + "# Casting (3)\n\n- [x] a\n  - [ ] sub\n- [ ] b", "Casting", "b", srcOK},
		{"all checked", head + done + "# Casting\n- [x] a\n- [x] b", "Casting", reviewStatus, srcOK},
		{"no boxes", head + done + "# Casting\nsome notes", "Casting", planStatus, srcOK},
		{"all finished", head + done, "All milestones finished.", "", srcEmpty},
		{"no milestones", head, "No milestones.", "", srcEmpty},
		{"first open child", head + "# Casting\n- [ ] first\n  - [x] a\n  - [ ] b", "Casting", "first > b", srcOK},
		{"children all done", head + "# Casting\n- [ ] first\n  - [x] a\n  - [x] b", "Casting", "first > " + reviewStatus, srcOK},
		{"checked parent skipped", head + "# Casting\n- [x] first\n  - [ ] a\n- [ ] second", "Casting", "second", srcOK},
		{"next milestone ends the run", head + "# Casting\n- [ ] first\n# Later\n  - [ ] a", "Casting", "first", srcOK},
	}
	for _, c := range cases {
		r := parseDispositio(c.body)
		if r.text != c.want || r.sub != c.sub || r.kind != c.kind {
			t.Errorf("%s: got %q/%q/%d", c.name, r.text, r.sub, r.kind)
		}
	}
}

func TestReadSource(t *testing.T) {
	home := t.TempDir()
	homeDirFn = func() (string, error) { return home, nil }
	t.Cleanup(func() { homeDirFn = os.UserHomeDir })

	os.WriteFile(filepath.Join(home, "todo.md"), []byte("- [ ] hi"), 0o644)

	cases := []struct {
		typ  sourceType
		cfg  string
		want string
		kind sourceKind
	}{
		{srcMarkdown, "~/todo.md", "hi", srcOK},
		{srcMarkdown, "todo.md", "hi", srcOK},
		{srcMarkdown, filepath.Join(home, "todo.md"), "hi", srcOK},
		{srcMarkdown, "nope.md", "File not found: nope.md", srcErr},
		{srcMarkdown, "  ", "Input a file", srcEmpty},
		{srcStatic, "Just do it", "Just do it", srcOK},
		{srcStatic, "", "Input text", srcEmpty},
	}
	for _, c := range cases {
		r := readSource(c.typ, c.cfg)
		if r.text != c.want || r.kind != c.kind {
			t.Errorf("%s %q: got %q/%d", c.typ, c.cfg, r.text, r.kind)
		}
	}
}

func TestSourceRows(t *testing.T) {
	l := statusLine{text: "main", sub: "under", style: srcStyle, subStyle: grayStyle}
	cases := []struct {
		name string
		line statusLine
		rows int
		want int
	}{
		{"both fit", l, 4, 2},
		{"no room for sub", l, 1, 1},
		{"no sub", statusLine{text: "main", style: srcStyle}, 4, 1},
		{"no room at all", l, 0, 0},
	}
	for _, c := range cases {
		if got := len(sourceRows(c.line, 40, 60, c.rows)); got != c.want {
			t.Errorf("%s: got %d rows, want %d", c.name, got, c.want)
		}
	}

	// the main line keeps its two rows and the sub never squeezes them out
	long := statusLine{text: strings.Repeat("word ", 40), sub: strings.Repeat("note ", 40), style: srcStyle, subStyle: grayStyle}
	if got := len(sourceRows(long, 40, 60, 4)); got != 4 {
		t.Errorf("long: got %d rows, want 4", got)
	}
	if got := len(sourceRows(long, 40, 60, 2)); got != 2 {
		t.Errorf("long capped: got %d rows, want 2", got)
	}
}
