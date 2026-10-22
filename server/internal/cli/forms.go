package cli

import (
	"io"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
)

type option struct{ label, value string }
type input struct {
	label, description string
	value              *string
	validate           func(string) error
}

// prompts keeps configuration transactions testable without a terminal renderer.
type prompts interface {
	selectItem(string, string, []option) (string, error)
	inputs(string, []input) error
	confirm(string, string) (bool, error)
	note(string, string) error
}

type charmUI struct {
	in  io.Reader
	out io.Writer
}

func (u charmUI) run(title string, fields ...huh.Field) error {
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc / ctrl+c", "返回 / 取消"))
	keys.Select.Up.SetHelp("↑", "上移")
	keys.Select.Down.SetHelp("↓", "下移")
	keys.Select.Filter.SetHelp("/", "筛选")
	keys.Select.Submit.SetHelp("enter", "选择")
	keys.Input.Next.SetHelp("tab / enter", "下一项")
	keys.Input.Prev.SetHelp("shift+tab", "上一项")
	keys.Input.Submit.SetHelp("enter", "完成")
	keys.Confirm.Toggle.SetHelp("←/→", "切换")
	keys.Confirm.Accept.SetHelp("y", "确认")
	keys.Confirm.Reject.SetHelp("n", "返回")
	keys.Confirm.Submit.SetHelp("enter", "选择")
	keys.Note.Submit.SetHelp("enter", "返回")
	return huh.NewForm(huh.NewGroup(fields...).Title(title + " · Esc 返回")).
		WithInput(u.in).WithOutput(u.out).
		WithTheme(huh.ThemeFunc(huh.ThemeCharm)).WithKeyMap(keys).
		WithShowHelp(true).WithShowErrors(true).Run()
}

func (u charmUI) selectItem(title, description string, options []option) (string, error) {
	items := make([]huh.Option[string], len(options))
	for i, o := range options {
		items[i] = huh.NewOption(o.label, o.value)
	}
	var selected string
	err := u.run("ESP32 Monitor · 配置管理", huh.NewSelect[string]().
		Title(title).Description(description).Options(items...).Value(&selected))
	return selected, err
}

func (u charmUI) inputs(title string, inputs []input) error {
	fields := make([]huh.Field, 0, len(inputs))
	for _, i := range inputs {
		fields = append(fields, huh.NewInput().Title(i.label).Description(i.description).
			Value(i.value).Validate(i.validate))
	}
	return u.run(title+" · Tab 下一项 / Shift+Tab 上一项", fields...)
}

func (u charmUI) confirm(title, description string) (bool, error) {
	value := false // Destructive actions and writes require explicit acceptance.
	err := u.run("确认操作", huh.NewConfirm().Title(title).Description(description).
		Affirmative("确认").Negative("返回").Value(&value))
	return value, err
}

func (u charmUI) note(title, description string) error {
	return u.run("ESP32 Monitor", huh.NewNote().Title(title).Description(description).
		Next(true).NextLabel("返回菜单"))
}
