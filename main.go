package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// @region app:entry -- CLI ARGS + MAIN

const helpText = `cycler — move through daily tasks in a timed cycle

usage:
  cycler      open today's cycle
  cycler -v   print version
  cycler -h   print this help

state lives in ~/.config/cycler
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
