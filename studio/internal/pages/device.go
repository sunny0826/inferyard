package pages

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/icons/lucide"
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
	t := tokens(c)
	if compactLayout(c) {
		ui.Column(c).Gap(t.Density.Tight).Children(func() {
			ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			ui.Text(c, value).FontFeatures("tnum").Selectable().Label(label + "：" + value)
		})
		return
	}
	ui.Row(c).AlignItems(ui.Start).Gap(t.Density.Section).Padding(t.Density.Hairline, 0).Children(func() {
		ui.Box(c).Width(c.Theme().Rem(10)).Shrink(0).Padding(2, 0, 0, 0).Children(func() {
			ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		ui.Text(c, value).Grow(1).MinWidth(0).FontFeatures("tnum").Selectable().Label(label + "：" + value)
	})
}
func (s *viewState) refreshDevice() {
	if s.simulate(previewRefreshDevice) {
		return
	}
	s.workAs("refresh-device", func() (string, *core.ApiError) { return "设备信息已更新", s.service.RefreshDevice() })
}

func (s *viewState) deviceCollectionBlocked() bool { return s.busy || s.device.Refreshing }

func (s *viewState) deviceRefreshButton(c *ui.Context, label string) {
	if refreshButton(c, label, s.device.Refreshing || s.actionBusy("refresh-device")).Disabled(s.deviceCollectionBlocked()).Clicked() {
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
	t := tokens(c)
	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		heading(c, "设备", 1).Grow(1)
		s.deviceRefreshButton(c, "刷新设备")
	})
	snapshot, parseError := parseDeviceSnapshot(s.device.Snapshot)
	if snapshot != nil && parseError == nil {
		deviceHardware(c, snapshot.object("hardware"))
		if rec, ok := core.ParseStarterRecommendation(s.device.Snapshot); ok {
			s.deviceRecommendationCard(c, rec)
		}
	}
	errorRecoveryBanner(c, s.device.LastError, func() {
		s.deviceRefreshButton(c, "重试")
		if button(c, "前往设置").Clicked() {
			s.selected = "settings"
		}
	})
	if parseError != nil {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateDanger,
			Message: "设备快照无法读取，请重新采集或检查设置。",
			Actions: func() {
				s.deviceRefreshButton(c, "重新采集设备")
				button(c, "检查设备设置").OnClick(func() { s.selected = "settings" })
			},
		})
		return
	}
	if snapshot == nil && s.device.LastError == nil && !s.device.Refreshing {
		emptyGuideCard(c, emptyGuideOptions{
			Icon:        lucide.Cpu,
			Title:       "尚无设备快照",
			Description: "采集本机硬件与 GPU 信息。",
			Surface:     emptyCard,
			Action: func() {
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.CardGap).Children(func() {
					if primaryButton(c, "采集设备信息").Disabled(s.deviceCollectionBlocked()).Clicked() {
						s.refreshDevice()
					}
					if button(c, "前往设置").Clicked() {
						s.selected = "settings"
					}
				})
			},
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
			if arch := strings.TrimSpace(fmt.Sprint(v["arch_family"])); v["arch_family"] != nil && arch != "" {
				name += " · " + arch
			}
			if v["cores"] != nil {
				name += " · " + v.value("cores") + " 核"
			}
			if v.flag("metal_supported") {
				name += " · 支持 Metal"
			}
			if drv := strings.TrimSpace(fmt.Sprint(v["driver_version"])); v["driver_version"] != nil && drv != "" {
				name += " · 驱动 " + drv
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
		if feats := hw.object("cpu_features"); len(feats) > 0 {
			var flags []string
			if feats.flag("avx2") {
				flags = append(flags, "AVX2")
			}
			if feats.flag("avx512f") || feats.flag("avx512") {
				flags = append(flags, "AVX-512")
			}
			if len(flags) > 0 {
				deviceRow(c, "指令集", strings.Join(flags, " · "))
			}
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
	})
}

func (s *viewState) deviceRecommendationCard(c *ui.Context, rec core.StarterRecommendation) {
	t := tokens(c)
	card(c, "推荐配置", func() {
		if rec.Status == "blocked" {
			deviceRow(c, "状态", deviceLabel(rec.Reason))
			return
		}
		engineText := starterEngineLabel(rec.Engine)
		if rec.Backend != "" {
			engineText += " · " + deviceLabel(rec.Backend)
		}
		deviceRow(c, "推荐引擎", engineText)
		if rec.ModelTitle != "" {
			modelText := rec.ModelTitle
			if ctx := starterContextLabel(rec.RecommendedContext); ctx != "" {
				modelText += " · " + ctx
			}
			deviceRow(c, "推荐模型", modelText)
		}
		if rec.EstimatedDownloadBytes > 0 {
			deviceRow(c, "预计下载", formatDownloadBytes(rec.EstimatedDownloadBytes))
		}
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			button(c, "前往下载").OnClick(func() { s.selected = "downloads" })
		})
	})
}
