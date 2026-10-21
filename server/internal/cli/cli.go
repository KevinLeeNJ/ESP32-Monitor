// Package cli implements the interactive configuration workflow without running a server.
package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"esp32-monitor/server/internal/config"
)

type ServeFunc func(config.Config) error

func Run(args []string, in io.Reader, out, errOut io.Writer, interactive bool, serve ServeFunc) error {
	if len(args) == 0 {
		return errors.New("usage: monitor-server {configure|check-config|serve} --config PATH")
	}
	action := args[0]
	if action != "configure" && action != "check-config" && action != "serve" {
		return errors.New("unknown command; use configure, check-config or serve")
	}
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.SetOutput(errOut)
	path := flags.String("config", "./config/monitor.yaml", "configuration file")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if action == "configure" {
		if !interactive {
			return errors.New("configure requires an interactive terminal")
		}
		return configure(*path, in, out)
	}
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	if action == "check-config" {
		_, err = fmt.Fprintln(out, "配置校验通过")
		return err
	}
	return serve(c)
}

type wizard struct {
	input *bufio.Reader
	out   io.Writer
}

func (w *wizard) ask(label, def string) (string, error) {
	if _, err := fmt.Fprintf(w.out, "%s [%s]: ", label, def); err != nil {
		return "", err
	}
	line, err := w.input.ReadString('\n')
	if err != nil {
		return "", errors.New("configuration cancelled (input ended)")
	}
	line = strings.TrimSpace(line)
	if line == "" {
		line = def
	}
	return line, nil
}
func (w *wizard) yes(label string, def bool) (bool, error) {
	d := "n"
	if def {
		d = "y"
	}
	v, e := w.ask(label+" (y/n)", d)
	if e != nil {
		return false, e
	}
	switch strings.ToLower(v) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, errors.New("请输入 y 或 n")
	}
}
func (w *wizard) base(c *config.Config) error {
	var err error
	if c.Listen, err = w.ask("监听地址", c.Listen); err != nil {
		return err
	}
	if c.Database, err = w.ask("SQLite 路径（相对于服务进程工作目录）", c.Database); err != nil {
		return err
	}
	if c.PublicBaseURL, err = w.ask("NAS 对外 Base URL", c.PublicBaseURL); err != nil {
		return err
	}
	return nil
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
	i := panelIndex(*c, id)
	if i >= 0 {
		c.Panel.Devices = append(c.Panel.Devices[:i], c.Panel.Devices[i+1:]...)
	}
}
func (w *wizard) device(c *config.Config, adding bool) (string, error) {
	id, err := w.ask("device_id", "")
	if err != nil {
		return "", err
	}
	if !config.ValidID(id) {
		return "", errors.New("device_id 格式无效")
	}
	idx := agentIndex(*c, id)
	if adding && idx >= 0 {
		return "", errors.New("device_id 已存在")
	}
	if !adding && idx < 0 {
		return "", errors.New("device_id 不存在")
	}
	pi := panelIndex(*c, id)
	d := config.DefaultDevice(id, id, "/")
	if pi >= 0 {
		d = c.Panel.Devices[pi]
	}
	if d.Name, err = w.ask("卡片名称（最多 8 个 ASCII 字符）", d.Name); err != nil {
		return "", err
	}
	show, err := w.yes("在面板展示", adding || pi >= 0)
	if err != nil {
		return "", err
	}
	position := 0
	if show {
		if d.DiskMountPath, err = w.ask("磁盘挂载点", d.DiskMountPath); err != nil {
			return "", err
		}
		count := len(c.Panel.Devices)
		if pi < 0 {
			count++
		}
		def := count
		if pi >= 0 {
			def = pi + 1
		}
		p, e := w.ask(fmt.Sprintf("展示顺序（1-%d）", count), strconv.Itoa(def))
		if e != nil {
			return "", e
		}
		position, e = strconv.Atoi(p)
		if e != nil || position < 1 || position > count {
			return "", errors.New("展示顺序无效")
		}
	}
	token := ""
	if adding {
		token, err = config.GenerateToken()
		if err != nil {
			return "", err
		}
	}
	// Apply to a candidate so a validation failure leaves the staged config untouched.
	candidate := *c
	candidate.Agents = append([]config.Agent(nil), c.Agents...)
	candidate.Panel.Devices = append([]config.Device(nil), c.Panel.Devices...)
	if adding {
		candidate.Agents = append(candidate.Agents, config.Agent{ID: id, WriteToken: token})
	}
	removePanel(&candidate, id)
	if show {
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
func (w *wizard) summary(c config.Config) {
	fmt.Fprintf(w.out, "监听: %s\n数据库: %s\nBase URL: %s\n已登记客户端: %d；展示设备: %d\n", c.Listen, c.Database, c.PublicBaseURL, len(c.Agents), len(c.Panel.Devices))
	for _, a := range c.Agents {
		fmt.Fprintf(w.out, "客户端: %s\n", a.ID)
	}
	for i, d := range c.Panel.Devices {
		fmt.Fprintf(w.out, "%d. %s (%s), disk=%s\n", i+1, d.Name, d.ID, d.DiskMountPath)
	}
}
func configure(path string, in io.Reader, out io.Writer) error {
	original, err := config.Read(path)
	if err != nil {
		return err
	}
	w := wizard{bufio.NewReader(in), out}
	c := config.Defaults()
	first := original == nil
	pending := map[string]bool{}
	showRead := first
	if first {
		if err = w.base(&c); err != nil {
			return err
		}
		c.ReadToken, err = config.GenerateToken()
		if err != nil {
			return err
		}
		for {
			add, e := w.yes("添加客户端（包含 NAS 本机）", true)
			if e != nil {
				return e
			}
			if !add {
				break
			}
			id, e := w.device(&c, true)
			if e != nil {
				fmt.Fprintln(out, e)
				continue
			}
			pending[id] = true
		}
	} else {
		c, err = config.Decode(original)
		if err != nil {
			return err
		}
	}
	for {
		w.summary(c)
		fmt.Fprintln(out, "1 编辑基础配置 | 2 添加客户端 | 3 编辑客户端/展示顺序 | 4 删除客户端 | 5 查看客户端 Token | 6 轮换客户端 Token | 7 查看面板 Token | 8 轮换面板 Token | s 保存 | q 取消")
		choice, e := w.ask("操作", "s")
		if e != nil {
			return e
		}
		switch choice {
		case "1":
			candidate := c
			if e = w.base(&candidate); e != nil {
				return e
			}
			if e = candidate.Validate(); e != nil {
				fmt.Fprintln(out, e)
			} else {
				c = candidate
			}
		case "2", "3":
			id, e := w.device(&c, choice == "2")
			if e != nil {
				fmt.Fprintln(out, e)
			} else if choice == "2" {
				pending[id] = true
			}
		case "4", "5", "6":
			id, e := w.ask("device_id", "")
			if e != nil {
				return e
			}
			idx := agentIndex(c, id)
			if idx < 0 {
				fmt.Fprintln(out, "device_id 不存在")
				continue
			}
			switch choice {
			case "4":
				c.Agents = append(c.Agents[:idx], c.Agents[idx+1:]...)
				removePanel(&c, id)
				delete(pending, id)
			case "5":
				fmt.Fprintf(out, "客户端 %s Token（暂存配置，保存并重启后生效）: %s\n", id, c.Agents[idx].WriteToken)
			case "6":
				token, e := config.GenerateToken()
				if e != nil {
					return e
				}
				c.Agents[idx].WriteToken = token
				pending[id] = true
			}
		case "7":
			fmt.Fprintf(out, "面板 Token（暂存配置，保存并重启后生效）: %s\n", c.ReadToken)
		case "8":
			c.ReadToken, e = config.GenerateToken()
			if e != nil {
				return e
			}
			showRead = true
		case "q":
			fmt.Fprintln(out, "已取消，配置未保存")
			return nil
		case "s":
			if e = c.Validate(); e != nil {
				fmt.Fprintln(out, e)
				continue
			}
			confirm, e := w.yes("校验通过，确认保存", true)
			if e != nil {
				return e
			}
			if !confirm {
				continue
			}
			if e = config.Save(path, c, original); e != nil {
				return e
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
			fmt.Fprintf(out, "启动: monitor-server serve --config %q\n修改配置后需校验并重启服务；将新 Token 复制到对应客户端或 ESP32。\n", path)
			return nil
		default:
			fmt.Fprintln(out, "未知操作")
		}
	}
}
