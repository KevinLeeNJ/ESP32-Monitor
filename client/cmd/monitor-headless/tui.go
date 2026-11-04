package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"esp32-monitor/client/internal/collector"
	v2 "esp32-monitor/client/internal/collector/uploadv2"

	tea "github.com/charmbracelet/bubbletea"
)

type setupUI interface {
	text(string) (string, error)
	metrics(*collector.V2Capabilities, *v2.UploadConfig) (*v2.UploadConfig, error)
}
type terminalUI struct {
	ctx context.Context
	in  io.Reader
	out io.Writer
}

func (u terminalUI) text(title string) (string, error) {
	m, err := u.run(promptModel{title: title})
	return strings.TrimSpace(string(m.input)), err
}
func (u terminalUI) run(model promptModel) (promptModel, error) {
	p := tea.NewProgram(model, tea.WithInput(u.in), tea.WithOutput(u.out), tea.WithContext(u.ctx))
	result, err := p.Run()
	if err != nil {
		return model, err
	}
	model = result.(promptModel)
	if model.canceled {
		return model, errors.New("setup canceled")
	}
	return model, nil
}

type promptModel struct {
	title    string
	input    []rune
	canceled bool
}

func (m promptModel) Init() tea.Cmd { return nil }
func (m promptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.canceled = true
			return m, tea.Quit
		case tea.KeyEnter:
			return m, tea.Quit
		case tea.KeyBackspace, tea.KeyDelete:
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}
		case tea.KeySpace:
			m.input = append(m.input, ' ')
		case tea.KeyRunes:
			for _, r := range key.Runes {
				if !unicode.IsControl(r) {
					m.input = append(m.input, r)
				}
			}
		}
	}
	return m, nil
}
func (m promptModel) View() string {
	return fmt.Sprintf("%s\n\n> %s_\n\nEnter: confirm · Esc: cancel\n", m.title, string(m.input))
}
