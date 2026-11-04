package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"esp32-monitor/client/internal/collector"
	v2 "esp32-monitor/client/internal/collector/uploadv2"
	tea "github.com/charmbracelet/bubbletea"
)

type metricSpec struct {
	key, title, group string
	members           []string
	requireAll        bool
}

// Keep these labels and bundles aligned with the desktop metrics page.
var metricSpecs = []metricSpec{
	{"cpu.usage", "总体 CPU 占用率", "CPU", nil, false},
	{"cpu.model", "CPU 信息", "CPU", []string{"cpu.model", "cpu.physical_cores", "cpu.logical_cores", "cpu.performance_cores", "cpu.efficiency_cores"}, false},
	{"core.usage", "逐核 CPU 占用率", "CPU", nil, false},
	{"cpu.load1", "CPU 负载", "CPU", []string{"cpu.load1", "cpu.load5", "cpu.load15"}, false},
	{"cpu.temperature", "CPU 平均温度", "CPU", nil, false},
	{"memory.usage", "内存", "内存（Swap）", []string{"memory.usage", "memory.used", "memory.total"}, false},
	{"memory.app", "内存构成", "内存（Swap）", []string{"memory.app", "memory.wired", "memory.compressed"}, false},
	{"memory.swap_total", "Swap", "内存（Swap）", []string{"memory.swap_total", "memory.swap_used"}, false},
	{"memory.pressure", "内存压力", "内存（Swap）", nil, false},
	{"gpu.usage", "GPU 占用率", "GPU", nil, false}, {"gpu.temperature", "GPU 平均温度", "GPU", nil, false},
	{"drive.model", "磁盘信息", "磁盘", []string{"drive.model", "drive.total", "drive.used"}, false},
	{"drive.read_rate", "磁盘读写", "磁盘", []string{"drive.read_rate", "drive.write_rate"}, false},
	{"drive.smart_health", "SMART 健康状态", "磁盘", nil, false},
	{"interface.medium", "网络连接", "网络", []string{"interface.medium", "interface.up"}, false},
	{"interface.upload_rate", "网络速度", "网络", []string{"interface.upload_rate", "interface.download_rate"}, false},
	{"interface.ipv4", "IP 地址", "网络", []string{"interface.ipv4", "network.public_ipv4"}, false},
	{"battery.level", "电池状态", "电池", []string{"battery.level", "battery.charging"}, true},
	{"system.uptime", "运行时间", "系统指标", nil, false},
	{"system.os", "设备信息", "系统指标", []string{"system.os", "system.product"}, false},
}
var metricParents = map[string]string{"gpu.temperature": "gpu.usage", "drive.read_rate": "drive.model", "drive.smart_health": "drive.model", "interface.upload_rate": "interface.medium", "interface.ipv4": "interface.medium"}

type metricRow struct {
	spec     metricSpec
	members  []collector.V2Metric
	objects  []uint16
	selected map[uint16]bool
	required bool
}
type metricLine struct {
	row   int
	id    uint16
	label string
}
type metricsModel struct {
	rows                  []metricRow
	names                 map[uint16]string
	groups                []string
	group, cursor, height int
	canceled              bool
	notice                string
}

func makeMetrics(caps *collector.V2Capabilities, previous *v2.UploadConfig) (metricsModel, error) {
	if caps == nil {
		return metricsModel{}, errors.New("无法读取指标能力")
	}
	m := metricsModel{names: map[uint16]string{}, height: 24}
	for _, o := range caps.Objects {
		m.names[o.ID] = o.Name
	}
	available := map[string]collector.V2Metric{}
	for _, v := range caps.Metrics {
		if v.Available {
			available[v.Metric] = v
		}
	}
	for _, spec := range metricSpecs {
		keys := spec.members
		if len(keys) == 0 {
			keys = []string{spec.key}
		}
		row := metricRow{spec: spec, selected: map[uint16]bool{}, required: spec.key == "cpu.usage" || spec.key == "memory.usage" || spec.key == "system.uptime"}
		for _, key := range keys {
			if v, ok := available[key]; ok {
				row.members = append(row.members, v)
			}
		}
		if len(row.members) == 0 && !row.required {
			continue
		}
		if spec.requireAll && len(row.members) != len(keys) {
			continue
		}
		first := true
		for _, v := range row.members {
			if len(v.Objects) == 0 {
				continue
			}
			if first {
				row.objects = slices.Clone(v.Objects)
				first = false
				continue
			}
			if spec.key == "drive.model" {
				for _, id := range v.Objects {
					if !slices.Contains(row.objects, id) {
						row.objects = append(row.objects, id)
					}
				}
			} else {
				row.objects = slices.DeleteFunc(row.objects, func(id uint16) bool { return !slices.Contains(v.Objects, id) })
			}
		}
		if !first && len(row.objects) == 0 {
			continue
		}
		if spec.key == "core.usage" {
			if len(caps.Inventory.Cores) == 0 || len(caps.Inventory.Cores) > 256 {
				continue
			}
			valid := true
			for _, core := range caps.Inventory.Cores {
				if !slices.Contains(row.objects, core.ID) {
					valid = false
				}
			}
			if !valid {
				continue
			}
			row.objects = nil
		}
		if previous != nil {
			for _, sel := range previous.Optional {
				if !slices.Contains(keys, sel.Metric) {
					continue
				}
				if len(row.objects) == 0 {
					row.selected[0] = true
				} else {
					for _, id := range sel.Objects {
						if slices.Contains(row.objects, id) {
							row.selected[id] = true
						}
					}
				}
			}
		}
		m.rows = append(m.rows, row)
		if !slices.Contains(m.groups, spec.group) {
			m.groups = append(m.groups, spec.group)
		}
	}
	if len(m.groups) == 0 {
		return m, errors.New("没有可配置的指标")
	}
	for i := range m.rows {
		parent := metricParents[m.rows[i].spec.key]
		if parent == "" {
			continue
		}
		for id := range m.rows[i].selected {
			found := false
			for _, r := range m.rows {
				if r.spec.key == parent && r.selected[id] {
					found = true
					break
				}
			}
			if !found {
				delete(m.rows[i].selected, id)
			}
		}
	}
	return m, nil
}
func (m metricsModel) lines() []metricLine {
	var lines []metricLine
	for i, r := range m.rows {
		if r.spec.group != m.groups[m.group] {
			continue
		}
		if len(r.objects) == 0 {
			lines = append(lines, metricLine{i, 0, r.spec.title})
			continue
		}
		for _, id := range r.objects {
			name := m.names[id]
			if name == "" {
				name = fmt.Sprintf("对象 %d", id)
			}
			lines = append(lines, metricLine{i, id, name})
		}
	}
	return lines
}
func (m metricsModel) Init() tea.Cmd { return nil }
func (m metricsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
	case tea.KeyMsg:
		lines := m.lines()
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.canceled = true
			return m, tea.Quit
		case tea.KeyEnter:
			return m, tea.Quit
		case tea.KeyLeft:
			if m.group > 0 {
				m.group--
				m.cursor = 0
				m.notice = ""
			}
		case tea.KeyRight:
			if m.group+1 < len(m.groups) {
				m.group++
				m.cursor = 0
				m.notice = ""
			}
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyDown:
			if m.cursor+1 < len(lines) {
				m.cursor++
			}
		case tea.KeySpace:
			if len(lines) == 0 {
				break
			}
			line := lines[m.cursor]
			r := &m.rows[line.row]
			if r.required {
				break
			}
			if parent := metricParents[r.spec.key]; parent != "" && !r.selected[line.id] {
				found := false
				for _, p := range m.rows {
					if p.spec.key == parent && p.selected[line.id] {
						found = true
						break
					}
				}
				if !found {
					m.notice = "请先启用该设备的主指标"
					break
				}
			}
			r.selected[line.id] = !r.selected[line.id]
			m.notice = ""
			if !r.selected[line.id] {
				for i := range m.rows {
					if metricParents[m.rows[i].spec.key] == r.spec.key {
						delete(m.rows[i].selected, line.id)
					}
				}
			}
		}
	}
	return m, nil
}
func (m metricsModel) View() string {
	var b strings.Builder
	b.WriteString("选择上传指标（必选项自动上传）\n\n")
	for i, g := range m.groups {
		if i == m.group {
			fmt.Fprintf(&b, "[%s] ", g)
		} else {
			fmt.Fprintf(&b, " %s  ", g)
		}
	}
	b.WriteString("\n\n")
	lines := m.lines()
	height := max(2, m.height-11)
	start := m.cursor
	// Include as many preceding items as fit, counting object headings.
	for start > 0 {
		candidate := start - 1
		count := 0
		last := -1
		for i := candidate; i <= m.cursor; i++ {
			if lines[i].id != 0 && lines[i].row != last {
				count++
			}
			count++
			last = lines[i].row
		}
		if count > height {
			break
		}
		start = candidate
	}
	end := start
	used := 0
	last := -1
	for end < len(lines) {
		extra := 1
		if lines[end].id != 0 && lines[end].row != last {
			extra++
		}
		if used+extra > height {
			break
		}
		used += extra
		last = lines[end].row
		end++
	}
	if start > 0 {
		b.WriteString("  ↑ 更多\n")
	}
	last = -1
	for i := start; i < end; i++ {
		line := lines[i]
		r := m.rows[line.row]
		if line.id != 0 && line.row != last {
			fmt.Fprintf(&b, "  %s\n", r.spec.title)
		}
		last = line.row
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}
		mark := " "
		if r.required || r.selected[line.id] {
			mark = "x"
		}
		suffix := ""
		if r.required {
			suffix = " 必选"
		}
		indent := ""
		if line.id != 0 {
			indent = "  "
		}
		fmt.Fprintf(&b, "%s %s[%s] %s%s\n", cursor, indent, mark, line.label, suffix)
	}
	if end < len(lines) {
		b.WriteString("  ↓ 更多\n")
	}
	if m.notice != "" {
		fmt.Fprintf(&b, "\n%s\n", m.notice)
	}
	if m.groups[m.group] == "网络" {
		b.WriteString("\n选择 IP 地址会通过外部服务查询公网地址。\n")
	}
	b.WriteString("\n←/→ 分类 · ↑/↓ 移动 · 空格 选择 · Enter 保存 · Esc 取消\n")
	return b.String()
}
func (m metricsModel) upload(previous *v2.UploadConfig) *v2.UploadConfig {
	interval := uint16(10)
	if previous != nil && previous.IntervalSeconds >= 5 && previous.IntervalSeconds <= 60 {
		interval = previous.IntervalSeconds
	}
	out := &v2.UploadConfig{Enabled: true, IntervalSeconds: interval}
	for _, r := range m.rows {
		if r.required {
			continue
		}
		for _, member := range r.members {
			if member.Required || member.Metric == "memory.used" || member.Metric == "memory.total" {
				continue
			}
			sel := v2.UploadSelection{Metric: member.Metric}
			if len(member.Objects) > 0 {
				if r.spec.key == "core.usage" && r.selected[0] {
					sel.Objects = slices.Clone(member.Objects)
				} else {
					for _, id := range member.Objects {
						if r.selected[id] {
							sel.Objects = append(sel.Objects, id)
						}
					}
				}
				if len(sel.Objects) == 0 {
					continue
				}
			} else if len(r.objects) > 0 {
				selected := false
				for _, yes := range r.selected {
					selected = selected || yes
				}
				if !selected {
					continue
				}
			} else if !r.selected[0] {
				continue
			}
			out.Optional = append(out.Optional, sel)
		}
	}
	return out
}
func (u terminalUI) metrics(caps *collector.V2Capabilities, previous *v2.UploadConfig) (*v2.UploadConfig, error) {
	model, err := makeMetrics(caps, previous)
	if err != nil {
		return nil, err
	}
	p := tea.NewProgram(model, tea.WithInput(u.in), tea.WithOutput(u.out), tea.WithContext(u.ctx))
	result, err := p.Run()
	if err != nil {
		return nil, err
	}
	model = result.(metricsModel)
	if model.canceled {
		return nil, errors.New("setup canceled")
	}
	return model.upload(previous), nil
}
