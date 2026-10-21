package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkdownTodo(t *testing.T) {
	cases := []struct {
		name, body, want string
		kind             sourceKind
	}{
		{"first unchecked", "# T\n- [x] done\n- [ ] next one\n- [ ] later", "next one", srcOK},
		{"nested and bullets", "* [x] a\n    + [ ] deep", "deep", srcOK},
		{"fenced ignored", "```\n- [ ] fake\n```\n- [ ] real", "real", srcOK},
		{"capital X is checked", "- [X] a\n- [ ] b", "b", srcOK},
		{"none", "- [x] a\n- [x] b", "No unchecked items.", srcEmpty},
	}
	for _, c := range cases {
		r := parseMarkdownTodo(c.body)
		if r.text != c.want || r.kind != c.kind {
			t.Errorf("%s: got %q/%d", c.name, r.text, r.kind)
		}
	}
}

func TestDrawdeck(t *testing.T) {
	cases := []struct {
		body, want string
		kind       sourceKind
	}{
		{"---\ncurrent: \"CHL Long Piece\"\n---\n# Focus", "CHL Long Piece", srcOK},
		{"---\ntitle: x\ncurrent: 'single'\n---", "single", srcOK},
		{"---\ncurrent: bare value\n---", "bare value", srcOK},
		{"---\ntitle: x\n---\ncurrent: outside", "No current item.", srcEmpty},
		{"---\ncurrent: \"\"\n---", "No current item.", srcEmpty},
		{"# no front matter", "No current item.", srcEmpty},
	}
	for _, c := range cases {
		r := parseDrawdeck(c.body)
		if r.text != c.want || r.kind != c.kind {
			t.Errorf("%q: got %q/%d", c.body, r.text, r.kind)
		}
	}
}

func TestDispositio(t *testing.T) {
	head := "```\nProject Name: X\n```\n\n"
	done := "# Old (2)\n```\nFinished: Apr 2 2026\n```\n\n- [ ] stray\n\n"
	cases := []struct {
		name, body, want string
		kind             sourceKind
	}{
		{"open task, duration stripped", head + done + "# Casting (3)\n\n- [x] a\n  - [ ] sub\n- [ ] b", "Casting: sub", srcOK},
		{"all checked", head + done + "# Casting\n- [x] a\n- [x] b", "Casting: Review and complete", srcOK},
		{"no boxes", head + done + "# Casting\nsome notes", "Casting: Plan", srcOK},
		{"all finished", head + done, "All milestones finished.", srcEmpty},
		{"no milestones", head, "No milestones.", srcEmpty},
	}
	for _, c := range cases {
		r := parseDispositio(c.body)
		if r.text != c.want || r.kind != c.kind {
			t.Errorf("%s: got %q/%d", c.name, r.text, r.kind)
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
