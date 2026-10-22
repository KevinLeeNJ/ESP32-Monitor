package cli

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"charm.land/huh/v2"
	"esp32-monitor/server/internal/config"
)

func clone(c config.Config) config.Config {
	c.Agents = append([]config.Agent(nil), c.Agents...)
	c.Panel.Devices = append([]config.Device(nil), c.Panel.Devices...)
	return c
}

func agentIndex(c config.Config, id string) int {
	for i, a := range c.Agents {
		if a.ID == id {
			return i
		}
	}
	return -1
}

func panelIndex(c config.Config, id string) int {
	for i, d := range c.Panel.Devices {
		if d.ID == id {
			return i
		}
	}
	return -1
}

func removePanel(c *config.Config, id string) {
	if i := panelIndex(*c, id); i >= 0 {
		c.Panel.Devices = append(c.Panel.Devices[:i], c.Panel.Devices[i+1:]...)
	}
}

func summary(c config.Config) string {
	var s strings.Builder
	fmt.Fprintf(&s, "监听 %s\n数据库 %s\nBase URL %s\n客户端 %d · 展示 %d/4\n", c.Listen, c.Database, c.PublicBaseURL, len(c.Agents), len(c.Panel.Devices))
	for i, d := range c.Panel.Devices {
		fmt.Fprintf(&s, "%d. %s (%s) · DISK %s\n", i+1, d.Name, d.ID, d.DiskMountPath)
	}
	return s.String()
}

func editBase(ui prompts, c *config.Config) error {
	candidate := clone(*c)
	inputs := []input{}
	for _, f := range []struct {
		label, description string
		value              *string
		set                func(*config.Config, string)
	}{
		{"监听地址", "host:port，例如 0.0.0.0:18080", &candidate.Listen, func(c *config.Config, v string) { c.Listen = v }},
		{"SQLite 路径", "相对路径以服务进程工作目录为准", &candidate.Database, func(c *config.Config, v string) { c.Database = v }},
		{"对外 Base URL", "例如 https://monitor.example.com:1443，不包含 API 路径", &candidate.PublicBaseURL, func(c *config.Config, v string) { c.PublicBaseURL = v }},
	} {
		inputs = append(inputs, input{f.label, f.description, f.value, func(v string) error {
			check := clone(*c)
			f.set(&check, v)
			return check.Validate()
		}})
	}
	if err := ui.inputs("基础配置", inputs); err != nil {
		return err
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	*c = candidate
	return nil
}

func chooseAgent(ui prompts, c config.Config) (string, error) {
	options := []option{{"返回主菜单", ""}}
	for _, a := range c.Agents {
		label := a.ID + " · 不展示"
		if i := panelIndex(c, a.ID); i >= 0 {
			label = fmt.Sprintf("%s · %s · 第 %d 张卡片", a.ID, c.Panel.Devices[i].Name, i+1)
		}
		options = append(options, option{label, a.ID})
	}
	return ui.selectItem("选择客户端", "选择已登记的设备，无需手动输入 ID", options)
}

func editDevice(ui prompts, c *config.Config, id string) (string, error) {
	adding := id == ""
	if adding {
		if err := ui.inputs("添加客户端", []input{{"客户端 ID", "字母或数字开头，最多 64 个字母、数字、点、下划线或连字符", &id, func(v string) error {
			if !config.ValidID(v) {
				return errors.New("客户端 ID 格式无效")
			}
			if agentIndex(*c, v) >= 0 {
				return errors.New("客户端 ID 已存在")
			}
			return nil
		}}}); err != nil {
			return "", err
		}
	}
	d := config.DefaultDevice(id, id, "/")
	pi := panelIndex(*c, id)
	if pi >= 0 {
		d = c.Panel.Devices[pi]
	}
	visibility := []option{{"在面板展示", "show"}, {"仅接收数据，不展示", "hide"}}
	if !adding && pi < 0 {
		visibility[0], visibility[1] = visibility[1], visibility[0]
	}
	show, err := ui.selectItem("客户端 "+id, "每台设备固定显示 CPU / MEM / DISK 和运行时间", visibility)
	if err != nil {
		return "", err
	}
	position := 0
	if show == "show" {
		count := len(c.Panel.Devices)
		if pi < 0 {
			count++
		}
		if count > 4 {
			return "", errors.New("面板最多展示 4 台设备；请先隐藏其他设备")
		}
		position = count
		if pi >= 0 {
			position = pi + 1
		}
		p := strconv.Itoa(position)
		if err = ui.inputs("卡片设置 · "+id, []input{
			{"卡片名称", "1–8 个可打印 ASCII 字符", &d.Name, validName},
			{"磁盘挂载点", "与 Telegraf 的 path 标签一致，如 /、/vol1 或 C:", &d.DiskMountPath, validMount},
			{"展示顺序", fmt.Sprintf("请输入 1–%d", count), &p, func(v string) error { _, e := parsePosition(v, count); return e }},
		}); err != nil {
			return "", err
		}
		position, err = parsePosition(p, count)
		if err != nil {
			return "", err
		}
	}
	candidate := clone(*c)
	if adding {
		token, e := config.GenerateToken()
		if e != nil {
			return "", e
		}
		candidate.Agents = append(candidate.Agents, config.Agent{ID: id, WriteToken: token})
	}
	removePanel(&candidate, id)
	if show == "show" {
		at := position - 1
		candidate.Panel.Devices = append(candidate.Panel.Devices, config.Device{})
		copy(candidate.Panel.Devices[at+1:], candidate.Panel.Devices[at:])
		candidate.Panel.Devices[at] = d
	}
	if err = candidate.Validate(); err != nil {
		return "", err
	}
	*c = candidate
	return id, nil
}

func validName(v string) error {
	if len(v) > 0 && len(v) <= 8 && strings.TrimSpace(v) != "" {
		valid := true
		for _, b := range []byte(v) {
			if b < 32 || b > 126 {
				valid = false
			}
		}
		if valid {
			return nil
		}
	}
	return errors.New("名称须为 1–8 个可打印 ASCII 字符")
}
func validMount(v string) error {
	if strings.TrimSpace(v) == "" || len(v) > 128 || strings.ContainsRune(v, 0) {
		return errors.New("请输入不超过 128 字节的磁盘路径")
	}
	return nil
}
func parsePosition(v string, count int) (int, error) {
	i, e := strconv.Atoi(v)
	if e != nil || i < 1 || i > count {
		return 0, fmt.Errorf("顺序须为 1–%d", count)
	}
	return i, nil
}

var mainOptions = []option{
	{"编辑基础配置", "base"}, {"添加客户端", "add"}, {"编辑客户端 / 卡片顺序", "edit"},
	{"删除客户端", "delete"}, {"查看客户端 Token", "view-agent"}, {"轮换客户端 Token", "rotate-agent"},
	{"查看面板 Token", "view-panel"}, {"轮换面板 Token", "rotate-panel"},
	{"保存并退出", "save"}, {"放弃修改并退出", "quit"},
}

func configure(path string, ui prompts, out io.Writer) error {
	original, err := config.Read(path)
	if err != nil {
		return err
	}
	c := config.Defaults()
	first := original == nil
	if first {
		c.ReadToken, err = config.GenerateToken()
	} else {
		c, err = config.Decode(original)
	}
	if err != nil {
		return err
	}
	baseline := clone(c)
	pending := map[string]bool{}
	showRead := first
	status := "↑↓ 选择 · Enter 确认 · Esc 返回；修改仅在保存后写入。"
	for {
		dirty := first || !reflect.DeepEqual(c, baseline)
		title := "配置管理 · " + path
		if dirty {
			title += " · 未保存"
		}
		action, e := ui.selectItem(title, summary(c)+"\n"+status, mainOptions)
		if errors.Is(e, huh.ErrUserAborted) {
			action = "quit"
			e = nil
		}
		if e != nil {
			return e
		}
		status = "修改暂存于内存；保存并重启 Server 后生效。"
		e = func() error {
			switch action {
			case "base":
				return editBase(ui, &c)
			case "add":
				id, e := editDevice(ui, &c, "")
				if e == nil {
					pending[id] = true
				}
				return e
			case "edit", "delete", "view-agent", "rotate-agent":
				id, e := chooseAgent(ui, c)
				if e != nil || id == "" {
					return e
				}
				i := agentIndex(c, id)
				switch action {
				case "edit":
					_, e = editDevice(ui, &c, id)
					return e
				case "view-agent":
					return ui.note("客户端 "+id+" 上传 Token", c.Agents[i].WriteToken+"\n暂存值；修改需保存并重启后生效。")
				case "delete", "rotate-agent":
					label := "删除客户端 " + id + "？"
					description := "保存并重启后，该客户端不能继续上传，卡片也将移除。"
					if action == "rotate-agent" {
						label = "轮换客户端 " + id + " 的 Token？"
						description = "保存并重启后，旧 Token 失效，需更新客户端配置。"
					}
					ok, e := ui.confirm(label, description)
					if e != nil || !ok {
						return e
					}
					if action == "delete" {
						c.Agents = append(c.Agents[:i], c.Agents[i+1:]...)
						removePanel(&c, id)
						delete(pending, id)
					} else {
						token, e := config.GenerateToken()
						if e != nil {
							return e
						}
						c.Agents[i].WriteToken = token
						pending[id] = true
					}
				}
			case "view-panel":
				return ui.note("面板只读 Token", c.ReadToken+"\nESP32 Base URL: "+c.PublicBaseURL+"\n暂存值；修改需保存并重启后生效。")
			case "rotate-panel":
				ok, e := ui.confirm("轮换面板只读 Token？", "保存并重启后需更新 ESP32 的 Token。")
				if e != nil || !ok {
					return e
				}
				token, e := config.GenerateToken()
				if e != nil {
					return e
				}
				c.ReadToken = token
				showRead = true
			case "save", "quit": // Handled below so successful exit is explicit.
			default:
				return errors.New("未知操作")
			}
			return nil
		}()
		if errors.Is(e, huh.ErrUserAborted) {
			status = "已返回，当前操作未应用。"
			continue
		}
		if e != nil {
			status = "操作未完成：" + e.Error()
			continue
		}
		if action == "quit" {
			if dirty {
				ok, e := ui.confirm("放弃所有未保存修改？", "磁盘上的配置保持不变。")
				if errors.Is(e, huh.ErrUserAborted) {
					continue
				}
				if e != nil {
					return e
				}
				if !ok {
					continue
				}
			}
			_, err = fmt.Fprintln(out, "已取消，配置未保存")
			return err
		}
		if action != "save" {
			continue
		}
		if e = c.Validate(); e != nil {
			status = "无法保存：" + e.Error()
			continue
		}
		ok, e := ui.confirm("确认保存配置？", summary(c)+"\n保存后需手动重启 Server；不会自动修改系统服务。")
		if errors.Is(e, huh.ErrUserAborted) {
			continue
		}
		if e != nil {
			return e
		}
		if !ok {
			continue
		}
		if err = config.Save(path, c, original); err != nil {
			return err
		}
		fmt.Fprintf(out, "配置已保存: %s\n", path)
		if showRead {
			fmt.Fprintf(out, "ESP32 Base URL: %s\n面板只读 Token: %s\n", c.PublicBaseURL, c.ReadToken)
		}
		for _, a := range c.Agents {
			if pending[a.ID] {
				fmt.Fprintf(out, "客户端 %s 上传 Token: %s\n", a.ID, a.WriteToken)
			}
		}
		_, err = fmt.Fprintf(out, "启动: monitor-server serve --config %q\n修改配置后需校验并重启服务；将新 Token 复制到对应客户端或 ESP32。\n", path)
		return err
	}
}
