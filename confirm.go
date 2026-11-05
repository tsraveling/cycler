package main

import tea "github.com/charmbracelet/bubbletea"

// @region ui:confirm -- REUSABLE YES/NO PROMPT

type confirmKind int

const (
	confirmComplete confirmKind = iota + 1
	confirmSkip
	confirmPass
	confirmFinish
	confirmDelete
	confirmDiscard
	confirmBlockFinish
	confirmStartNext
)

type confirmResult int

const (
	confirmPending confirmResult = iota
	confirmYes
	confirmNo
)

// owns the prompt and key interpretation; placement stays with the caller
type confirm struct {
	kind   confirmKind
	prompt string
	// the block this was raised against, fixed at the moment it was raised. a
	// prompt answered after the clock has moved on must still act on what it
	// named, not on whatever is current by then.
	idx int
}

func newConfirm(kind confirmKind, prompt string) *confirm {
	return &confirm{kind: kind, prompt: prompt, idx: -1}
}

func newBlockConfirm(kind confirmKind, prompt string, idx int) *confirm {
	return &confirm{kind: kind, prompt: prompt, idx: idx}
}

// y confirms, any other key cancels
func (c *confirm) handle(msg tea.Msg) confirmResult {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return confirmPending
	}
	if km.String() == "y" {
		return confirmYes
	}
	return confirmNo
}
