package pages

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

type deviceObject map[string]any

func (v deviceObject) object(key string) deviceObject {
	value, _ := v[key].(map[string]any)
	return deviceObject(value)
}
func (v deviceObject) list(key string) []any { value, _ := v[key].([]any); return value }
func (v deviceObject) flag(key string) bool  { value, _ := v[key].(bool); return value }
func (v deviceObject) value(key string) string {
	if v[key] == nil {
		return "缺测"
	}
	return fmt.Sprint(v[key])
}
func (v deviceObject) label(key string) string { return deviceLabel(v.value(key)) }
func deviceItem(value any) deviceObject        { item, _ := value.(map[string]any); return deviceObject(item) }

func deviceBytes(value any) string {
	n, ok := value.(json.Number)
	if !ok {
		return "缺测"
	}
	v, err := n.Int64()
	if err != nil {
		return "缺测"
	}
	for _, unit := range []struct {
		size int64
		name string
	}{{1 << 40, "TiB"}, {1 << 30, "GiB"}, {1 << 20, "MiB"}, {1 << 10, "KiB"}} {
		if v >= unit.size {
			return fmt.Sprintf("%.2f %s", float64(v)/float64(unit.size), unit.name)
		}
	}
	return fmt.Sprintf("%d bytes", v)
}
func deviceMeasurement(v deviceObject, key string, capacity bool) string {
	value := v.value(key)
	if capacity {
		value = deviceBytes(v[key])
	}
	if value == "缺测" && v.object("missing")[key] != nil {
		value += " — " + v.object("missing").label(key)
	}
	return value
}
func deviceRow(c *ui.Context, label, value string) {
	ui.Row(c).Gap(16).Children(func() {
		ui.Text(c, label).Width(150).TextColor(c.Theme().TextMuted)
		ui.Text(c, value).Grow(1).MinWidth(0).Selectable()
	})
}
func deviceFold(c *ui.Context, title string, fn func()) {
	container := ui.Column(c.Key(title))
	open := ui.Local(container, "open", func() bool { return false })
	container.Children(func() { ui.SidebarSection(c, title, open, fn) })
}
func deviceMono(c *ui.Context, value string) {
	ui.Text(c, value).Font("monospace").TextColor(c.Theme().TextMuted).Selectable()
}
func (s *viewState) refreshDevice() {
	// RefreshDevice 异步执行：同步返回 nil 仅表示采集已开始，成败经 LastError 呈现，
	// 因此这里用中性提示，不能承诺"已更新"。
	s.work(func() (string, *core.ApiError) { return "正在采集设备信息…", s.service.RefreshDevice() })
}
func (s *viewState) deviceRefreshButton(c *ui.Context, label string) {
	if ui.PrimaryButton(c, label).AlignSelf(ui.Start).Disabled(s.busy || s.device.Refreshing).Clicked() {
		s.refreshDevice()
	}
}
func parseDeviceSnapshot(raw json.RawMessage) (deviceObject, error) {
	var snapshot deviceObject
	raw = bytes.TrimSpace(raw)
	var parseError error
	if len(raw) > 0 && string(raw) != "null" {
		if !json.Valid(raw) {
			parseError = fmt.Errorf("invalid snapshot")
		} else {
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			parseError = decoder.Decode(&snapshot)
		}
	}
	return snapshot, parseError
}

func (s *viewState) deviceView(c *ui.Context) {
	ui.Row(c).Children(func() {
		ui.Text(c, "设备").Bold().FontSize(24).Grow(1)
		if s.device.Refreshing {
			ui.Text(c, "正在采集设备信息…").TextColor(c.Theme().TextMuted)
		}
		s.deviceRefreshButton(c, "刷新设备")
	})
	if s.progress != nil && s.progress.NextAction == "device_check" {
		card(c, "检查设备", func() {
			ui.Text(c, "模型与引擎可在「模型与下载」页准备。")
			if ui.PrimaryButton(c, "检查设备").AlignSelf(ui.Start).Disabled(s.busy).Clicked() {
				s.execute(s.nextCommand(), false)
			}
		})
	}
	snapshot, parseError := parseDeviceSnapshot(s.device.Snapshot)
	if snapshot != nil && parseError == nil {
		deviceHardware(c, snapshot.object("hardware"))
	}
	errorView(c, s.device.LastError)
	if s.device.LastError != nil {
		ui.Row(c).Gap(10).Children(func() {
			s.deviceRefreshButton(c, "重试")
			if ui.Button(c, "前往设置").Clicked() {
				s.selected = "settings"
			}
		})
	}
	if parseError != nil {
		ui.Text(c, "设备快照无法读取。").TextColor(c.Theme().Danger)
		return
	}
	if snapshot == nil && s.device.LastError == nil {
		ui.Column(c).AlignItems(ui.Center).Gap(12).Padding(40).Background(c.Theme().Surface).Radius(12).Children(func() {
			ui.Text(c, "尚无设备快照").Bold().FontSize(18)
			ui.Text(c, "设备检查会采集硬件、GPU 与本地模型信息，不会向模型发送请求。")
			if ui.Button(c, "前往设置配置模型目录").Clicked() {
				s.selected = "settings"
			}
		})
	}
}
func deviceHardware(c *ui.Context, hw deviceObject) {
	card(c, "硬件", func() {
		deviceRow(c, "处理器", deviceMeasurement(hw, "cpu_model", false)+" · "+deviceMeasurement(hw, "logical_cpus", false)+" 核")
		memory := deviceMeasurement(hw, "memory_total_bytes", true) + "（当前可用约 " + deviceMeasurement(hw, "memory_available_bytes", true)
		if hw.flag("memory_available_is_estimate") {
			memory += "，估算"
		}
		deviceRow(c, "内存", memory+"）")
		gpu := hw.object("gpu")
		devices := gpu.list("devices")
		for i, item := range devices {
			v := deviceItem(item)
			name := v.value("name")
			if v["cores"] != nil {
				name += " · " + v.value("cores") + " 核"
			}
			if v.flag("metal_supported") {
				name += " · 支持 Metal"
			}
			label := "GPU"
			if len(devices) > 1 {
				label = fmt.Sprintf("GPU %d", i+1)
			}
			deviceRow(c, label, name)
			deviceRow(c, label+" 显存", deviceMeasurement(v, "memory_total_bytes", true))
		}
		if len(devices) == 0 {
			value := "缺测"
			if gpu["reason"] != nil {
				value += " — " + gpu.label("reason")
			}
			deviceRow(c, "GPU", value)
		}
		deviceRow(c, "磁盘可用", deviceMeasurement(hw, "disk_free_bytes", true))
		power := deviceMeasurement(hw, "ac_online", false)
		if online, ok := hw["ac_online"].(bool); ok {
			power = "使用电池"
			if online {
				power = "已接通"
			}
		}
		deviceRow(c, "电源", power)
		architecture := hw.value("architecture")
		if hw.flag("apple_silicon") {
			architecture += "（Apple 芯片）"
		}
		deviceRow(c, "架构", architecture)
		deviceFold(c, "数据来源与缺测明细", func() {
			sources := hw.object("sources")
			for _, key := range deviceKeys(sources) {
				deviceMono(c, key+" ← "+sources.value(key))
			}
			deviceMissing(c, "", hw.object("missing"))
			for i, item := range devices {
				deviceMissing(c, fmt.Sprintf("GPU %d · ", i+1), deviceItem(item).object("missing"))
			}
			deviceMono(c, "检测位置："+hw.value("disk_path"))
		})
	})
}
func deviceKeys(v deviceObject) []string {
	keys := make([]string, 0, len(v))
	for key := range v {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func deviceMissing(c *ui.Context, prefix string, missing deviceObject) {
	for _, key := range deviceKeys(missing) {
		deviceMono(c, prefix+key+"：缺测 — "+missing.label(key))
	}
}
