package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// @region app:entry -- CLI ARGS + MAIN

const helpText = `cycler — move through daily tasks in a timed cycle

usage:
  cycler                open today's cycle
  cycler -v, --version  print version
  cycler -h, --help     print this help

cycler keys:
  space   start / pause; on DONE, move to the next item
  enter   complete early (y/n); on DONE, move to the next item
  s       skip item (y/n)
  F       finish day (y/n)
  c       configure items
  ?       toggle full helpline
  q       quit (a running timer is saved paused)

configure keys:
  a             add item
  e, enter      edit item
  j/k, ↑/↓      select
  J/K, ⇧↑/⇧↓    reorder
  d             delete (y/n)
  esc           back

item form:
  tab / shift+tab   next / previous field
  enter             next field; on save, save
  h/l, ←/→          change source type, or toggle save/cancel
  esc               cancel (asks to discard changes)

sources:
  Markdown TODO   first unchecked checkbox in a file
  Drawdeck        "current" front matter value
  Dispositio      "{milestone}: {task}" from the first unfinished milestone
  Static          fixed text

paths accept ~ and resolve relative to $HOME.
state lives in ~/.config/cycler. the day rolls over at 4am.
`

func main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-h", "--help":
			fmt.Print(helpText)
			return
		case "-v", "--version":
			fmt.Println(Version)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown argument: %s\n", arg)
			os.Exit(1)
		}
	}

	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
