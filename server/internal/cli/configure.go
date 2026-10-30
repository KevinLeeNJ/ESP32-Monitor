package cli

import (
	"errors"
	"fmt"
	"io"
	"reflect"

	"charm.land/huh/v2"
	"esp32-monitor/server/internal/config"
)

func summary(c config.Config) string {
	return fmt.Sprintf("监听 %s\n数据库 %s\nBase URL %s\n设备注册、凭据和面板选项在 WebUI 管理。", c.Listen, c.Database, c.PublicBaseURL)
}
func editBase(ui prompts, c *config.Config) error {
	candidate := *c
	inputs := []input{}
	for _, f := range []struct {
		label, description string
		value              *string
		set                func(*config.Config, string)
	}{
		{"监听地址", "默认 127.0.0.1:8080，通过 HTTPS 反代暴露公网", &candidate.Listen, func(c *config.Config, v string) { c.Listen = v }},
		{"SQLite 路径", "相对路径以进程工作目录为准；包含凭据与指标", &candidate.Database, func(c *config.Config, v string) { c.Database = v }},
		{"对外 Base URL", "公网 HTTPS 地址，不包含 API 路径", &candidate.PublicBaseURL, func(c *config.Config, v string) { c.PublicBaseURL = v }},
	} {
		inputs = append(inputs, input{f.label, f.description, f.value, func(v string) error { check := *c; f.set(&check, v); return check.Validate() }})
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

var mainOptions = []option{{"编辑基础配置", "base"}, {"查看面板 Token", "view-panel"}, {"轮换面板 Token", "rotate-panel"}, {"保存并退出", "save"}, {"放弃修改并退出", "quit"}}

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
	baseline := c
	// v1 config can be read for migration. Device tokens and mount paths are never
	// imported into v2, where registration owns identity and disks have stable IDs.
	legacy := len(c.Agents) > 0 || len(c.Panel.Devices) > 0
	if legacy {
		c.Agents = nil
		c.Panel.Devices = nil
	}
	status := "↑↓ 选择 · Enter 确认 · Esc 返回；修改仅在保存后写入。"
	if legacy {
		status = "旧版设备配置将在保存时移除，请通过 WebUI 重新注册设备。"
	}
	showRead := first
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
			case "view-panel":
				return ui.note("面板只读 Token", c.ReadToken+"\n摘要地址: "+c.PublicBaseURL+"/v2/devices/summary\n此凭据不用于 WebUI 登录。")
			case "rotate-panel":
				ok, err := ui.confirm("轮换面板只读 Token？", "保存并重启后需更新读取摘要的设备凭据。")
				if err != nil || !ok {
					return err
				}
				token, err := config.GenerateToken()
				if err != nil {
					return err
				}
				c.ReadToken = token
				showRead = true
			case "save", "quit":
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
				ok, err := ui.confirm("放弃所有未保存修改？", "磁盘上的配置保持不变。")
				if errors.Is(err, huh.ErrUserAborted) {
					continue
				}
				if err != nil {
					return err
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
		ok, e := ui.confirm("确认保存配置？", summary(c)+"\n保存后需手动重启 Server。")
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
			fmt.Fprintf(out, "摘要地址: %s/v2/devices/summary\n面板只读 Token: %s\n", c.PublicBaseURL, c.ReadToken)
		}
		_, err = fmt.Fprintf(out, "启动: monitor-server serve --config %q\n设备请在 WebUI 注册；首次 WebUI 密码在启动终端显示。\n", path)
		return err
	}
}
