package main

import tea "github.com/charmbracelet/bubbletea"

// @region ui:confirm -- REUSABLE YES/NO PROMPT

type confirmKind int

const (
	confirmComplete confirmKind = iota + 1
	confirmSkip
	confirmFinish
	confirmDelete
	confirmDiscard
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
}

func newConfirm(kind confirmKind, prompt string) *confirm {
	return &confirm{kind: kind, prompt: prompt}
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
