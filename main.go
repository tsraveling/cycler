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
  c       complete early (y/n); on DONE, move to the next item
  s       skip item (y/n)
  p       pass a rationed item for this cycle (y/n)
  f       finish the current block early (y/n)
  e       extend the current block
  F       finish day (y/n)
  C       configure items and blocks
  ?       toggle full helpline
  q       quit (a running timer is saved paused)

configure keys:
  tab           switch between Cycler Config and Block Admin
  a             add
  o             insert after the selection
  e, enter      edit
  j/k, ↑/↓      select
  J/K, ⇧↑/⇧↓    reorder (items only)
  d             delete (y/n)
  esc           back

item form:
  Name              what the item is called
  Timer             minutes and seconds
  Times per day     repeats allowed today; blank is no limit
  Active on         the weekdays the item runs on
  Source type       where the item's current task is read from
  Source config     a path, or the text for a static item

block form:
  Title             what the block is called
  Type              Standard shows a clock; Cycler runs the cycler
  Color             the block's color; Standard blocks only
  Start, End        24-hour hh:mm
  Active on         the weekdays the block runs on
  Source type       where the block's current task is read from
  Source config     a path, or the text for a static block

form keys:
  j/k, ↑/↓          move between lines
  enter             start editing a line; commit and blur when editing
  esc               commit and blur when editing, otherwise cancel
  tab / shift+tab   next / previous input
  h/l, ←/→          change a picker, move the day cursor,
                    or toggle save/cancel
  space             toggle a day, or apply Every day / Weekdays

blocks:
  a block is a titled window of wall-clock time on a set of weekdays.
  with no blocks configured, cycler behaves as it always has. with
  blocks, the day reads: Sleep before the first, the block you are in,
  FREE TIME between blocks, and Day Done after the last.

  f ends the current block now and pulls the next one forward. e
  extends it, cancelling any block the extension swallows. a cycler
  block whose end passes while its timer is still running is held open
  until the item is finished.

sources:
  Markdown TODO   first unchecked checkbox in a file
  Drawdeck        "current" front matter value
  Dispositio      "{milestone}: {task}" from the first unfinished milestone
  Static          fixed text

an item runs only on its active weekdays, and only until it has
used up its daily repeats; a skip spends a repeat, a pass does not.
when nothing is left to run, the cycler shows an empty day.

paths accept ~ and resolve relative to $HOME.
state lives in ~/.config/cycler. the day rolls over at midnight.
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
