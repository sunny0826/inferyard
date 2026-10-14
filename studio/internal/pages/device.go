package pages

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

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
func (s *viewState) deviceRunsButton(c *ui.Context) {
	if ui.PrimaryButton(c, "前往运行页").Clicked() {
		s.selected = "runs"
	}
}
func (s *viewState) deviceView(c *ui.Context) {
	s.header(c, "设备")
	if s.progress != nil && s.progress.NextAction == "device_check" {
		card(c, "检查设备", func() {
			ui.Text(c, "模型与引擎可在「模型与下载」页准备。")
			if ui.PrimaryButton(c, "检查设备").AlignSelf(ui.Start).Disabled(s.busy).Clicked() {
				s.execute(s.nextCommand(), false)
			}
		})
	}
	var snapshot deviceObject
	raw := bytes.TrimSpace(s.device.Snapshot)
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
	if snapshot != nil && parseError == nil {
		s.deviceConclusion(c, snapshot)
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
	if snapshot == nil {
		if s.device.LastError == nil {
			ui.Column(c).AlignItems(ui.Center).Gap(12).Padding(40).Background(c.Theme().Surface).Radius(12).Children(func() {
				ui.Text(c, "尚无设备快照").Bold().FontSize(18)
				ui.Text(c, "设备检查会采集硬件、GPU 与本地模型信息，不会向模型发送请求。")
				if s.device.Refreshing {
					ui.Text(c, "正在采集设备信息…").TextColor(c.Theme().TextMuted)
				}
				ui.Row(c).Gap(10).Children(func() {
					s.deviceRefreshButton(c, "刷新设备")
					if ui.Button(c, "前往设置配置模型目录").Clicked() {
						s.selected = "settings"
					}
				})
			})
		}
		return
	}
	s.deviceNextSteps(c, snapshot)
	deviceRecommendation(c, snapshot.object("recommendation"))
	deviceHardware(c, snapshot.object("hardware"))
	deviceModels(c, snapshot.list("models"))
	deviceCapabilities(c, snapshot.object("platform_capabilities"))
}
func (s *viewState) deviceConclusion(c *ui.Context, snapshot deviceObject) {
	ui.Column(c).Gap(12).Padding(18).Background(c.Theme().Surface).Radius(12).Children(func() {
		if snapshot.flag("ready_to_run") {
			ui.Badge(c, "✓ 可运行").AlignSelf(ui.Start).TextColor(c.Theme().Success)
		} else {
			ui.Badge(c, "✕ 暂不可运行").AlignSelf(ui.Start).TextColor(c.Theme().Danger)
		}
		rec := snapshot.object("recommendation")
		if selected := rec.object("selected"); selected != nil && rec.value("status") != "blocked" {
			ui.Text(c, fmt.Sprintf("推荐方案：%s · %s · 上下文 %s · %s 线程", selected.value("model_name"), selected.label("mode"), selected.value("context_size"), selected.value("threads")))
		}
		if !snapshot.flag("ready_to_run") {
			var reasons []string
			seen := map[string]bool{}
			add := func(reason string) {
				if !seen[reason] {
					seen[reason] = true
					reasons = append(reasons, reason)
				}
			}
			if rec["reason"] != nil {
				add(rec.label("reason"))
			}
			for _, reason := range rec.list("limitations") {
				add(deviceLabel(fmt.Sprint(reason)))
			}
			for _, step := range snapshot.list("next_steps") {
				if v := deviceItem(step); v["reason"] != nil {
					add(v.label("reason"))
				}
			}
			if len(reasons) > 0 {
				ui.Text(c, "原因："+strings.Join(reasons, "、")).TextColor(c.Theme().Warning)
			}
		}
		ui.Row(c).Gap(12).Children(func() {
			captured := text(s.device.CapturedAt)
			if t, err := time.Parse(time.RFC3339, captured); err == nil {
				captured = t.Local().Format("2006-01-02 15:04:05")
			}
			requests := "模型请求数：" + snapshot.value("model_requests_sent")
			if snapshot.value("model_requests_sent") == "0" {
				requests = "本次检查未发送模型请求"
			}
			meta := "快照 " + captured + " · " + requests
			if s.device.Refreshing {
				meta = "正在采集设备信息…"
			}
			ui.Text(c, meta).Grow(1).TextColor(c.Theme().TextMuted)
			s.deviceRefreshButton(c, "刷新设备")
		})
	})
}
func (s *viewState) deviceNextSteps(c *ui.Context, snapshot deviceObject) {
	card(c, "下一步", func() {
		if snapshot.flag("ready_to_run") {
			ui.Row(c).Gap(12).Children(func() {
				ui.Text(c, "设备检查已通过，前往运行页创建候选并运行题包").Grow(1)
				s.deviceRunsButton(c)
			})
			return
		}
		for i, item := range snapshot.list("next_steps") {
			step := deviceItem(item)
			ui.Column(c.Key(fmt.Sprint(i))).Gap(6).Children(func() {
				ui.Row(c).Gap(12).Children(func() {
					ui.Text(c, fmt.Sprintf("%d.", i+1))
					switch step.value("action") {
					case "configure_and_bind_existing_service":
						ui.Text(c, "绑定已有的模型服务（需要先完成实时预检）").Grow(1)
						s.deviceRunsButton(c)
					case "provide_local_gguf":
						ui.Text(c, step.label("action")+"（"+step.label("reason")+"）").Grow(1)
						if ui.PrimaryButton(c, "前往模型与下载页").Clicked() {
							s.selected = "models"
						}
					case "probe":
						if command := step.list("command"); len(command) > 0 {
							ui.Text(c, "或先用探测验证候选配置：")
							parts := make([]string, len(command))
							for i, part := range command {
								parts[i] = fmt.Sprint(part)
							}
							line := strings.Join(parts, " ")
							ui.Text(c, line).Font("monospace").SingleLine().Grow(1).Selectable()
							if ui.Button(c, "复制").Clicked() {
								c.WriteClipboard(line)
							}
						} else {
							ui.Text(c, step.value("action")+" · "+step.label("reason"))
						}
					default:
						ui.Text(c, step.label("action")+" · "+step.label("reason"))
					}
				})
				if step.value("action") == "probe" && len(step.list("command")) > 0 {
					ui.Text(c, "CONFIG.toml 为占位符，请替换为真实配置路径。").TextColor(c.Theme().TextMuted)
				}
			})
		}
	})
}
func deviceRecommendation(c *ui.Context, rec deviceObject) {
	card(c, "推荐配置", func() {
		selected := rec.object("selected")
		if rec.value("status") == "blocked" || selected == nil {
			ui.Text(c, "状态："+rec.label("status"))
			ui.Text(c, "原因："+rec.label("reason")).TextColor(c.Theme().Warning)
			return
		}
		mode := selected.label("mode")
		if selected["gpu_name"] != nil {
			mode += "（" + selected.value("gpu_name") + "）"
		}
		for _, row := range [][2]string{{"模型", selected.value("model_name")}, {"加速方式", mode}, {"上下文长度", selected.value("context_size")}, {"线程数", selected.value("threads")}, {"所需可用内存", "≥ " + deviceBytes(selected["min_available_memory_bytes"])}, {"所需磁盘空间", "≥ " + deviceBytes(selected["min_disk_bytes"])}, {"推荐依据", selected.label("reason")}} {
			deviceRow(c, row[0], row[1])
		}
		for _, reason := range rec.list("limitations") {
			ui.Text(c, "⚠ "+deviceLabel(fmt.Sprint(reason))).TextColor(c.Theme().Warning)
		}
		deviceFold(c, "模型文件与原始参数", func() {
			deviceMono(c, selected.value("model_path"))
			for _, candidate := range rec.list("candidates") {
				other := deviceItem(candidate)
				if !reflect.DeepEqual(other, selected) {
					ui.Text(c, other.value("model_name")+" · "+other.label("mode"))
				}
			}
		})
	})
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
			deviceRow(c, "GPU", "缺测")
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
func deviceModels(c *ui.Context, models []any) {
	card(c, fmt.Sprintf("本地模型 %d 个 GGUF", len(models)), func() {
		for i, item := range models {
			model := deviceItem(item)
			ui.Column(c.Key(fmt.Sprint(i))).Gap(6).Children(func() {
				ui.Row(c).Gap(10).Children(func() {
					name := model.value("name")
					if model["name"] == nil {
						name = filepath.Base(model.value("path"))
					}
					ui.Text(c, name).Bold()
					if model.value("status") == "available" {
						ui.Text(c, deviceBytes(model["size_bytes"])+" · "+model.value("architecture")+" · 上下文 "+model.value("context_length")).TextColor(c.Theme().TextMuted)
						ui.Badge(c, "可用").TextColor(c.Theme().Success)
					} else {
						ui.Badge(c, model.label("status")).TextColor(c.Theme().Warning)
					}
				})
				if model["reason"] != nil {
					ui.Text(c, model.label("reason")).TextColor(c.Theme().Warning)
				}
				deviceMono(c, model.value("path"))
			})
		}
	})
}
func deviceCapabilities(c *ui.Context, capabilities deviceObject) {
	card(c, "平台能力", func() {
		items := [][2]string{{"device_inspection", "设备检查"}, {"live_single_run", "单次真实运行"}, {"live_experiments", "真实实验"}, {"metal_backend_verified", "Metal 后端"}}
		for i := 0; i < len(items); i += 2 {
			ui.Row(c).Gap(24).Children(func() {
				for _, item := range items[i : i+2] {
					ui.Row(c).Grow(1).Basis(0).Gap(8).Children(func() {
						ui.Text(c, item[1])
						if item[0] == "metal_backend_verified" {
							if capabilities.flag(item[0]) {
								ui.Badge(c, "已验证").TextColor(c.Theme().Success)
							} else {
								ui.Badge(c, "未验证").TextColor(c.Theme().TextMuted)
								ui.Text(c, "绑定服务后验证").TextColor(c.Theme().TextMuted)
							}
						} else if capabilities.flag(item[0]) {
							ui.Badge(c, "支持").TextColor(c.Theme().Success)
						} else {
							ui.Badge(c, "不支持").TextColor(c.Theme().TextMuted)
						}
					})
				}
			})
		}
	})
}
