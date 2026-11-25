package pages

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

type deviceObject map[string]any

func (v deviceObject) object(key string) deviceObject {
	value, _ := v[key].(map[string]any)
	return deviceObject(value)
}
func (v deviceObject) list(key string) []any { value, _ := v[key].([]any); return value }
func (v deviceObject) flag(key string) bool  { value, _ := v[key].(bool); return value }
func (v deviceObject) value(key string) string {
	return v.valueWithMissing(key, l10n.TR("device.missing"))
}
func (v deviceObject) valueFor(c *ui.Context, key string) string {
	return v.valueWithMissing(key, tr(c, "device.missing"))
}
func (v deviceObject) valueWithMissing(key, missing string) string {
	if v[key] == nil {
		return missing
	}
	return fmt.Sprint(v[key])
}
func (v deviceObject) label(key string) string { return deviceLabel(v.value(key)) }
func (v deviceObject) labelFor(c *ui.Context, key string) string {
	return deviceLabelFor(c, v.valueFor(c, key))
}
func deviceItem(value any) deviceObject { item, _ := value.(map[string]any); return deviceObject(item) }

func deviceBytes(value any) string {
	return deviceBytesWithMissing(value, l10n.TR("device.missing"))
}
func deviceBytesFor(c *ui.Context, value any) string {
	return deviceBytesWithMissing(value, tr(c, "device.missing"))
}
func deviceBytesWithMissing(value any, missing string) string {
	n, ok := value.(json.Number)
	if !ok {
		return missing
	}
	v, err := n.Int64()
	if err != nil {
		return missing
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
	missing := l10n.TR("device.missing")
	value := v.valueWithMissing(key, missing)
	if capacity {
		value = deviceBytesWithMissing(v[key], missing)
	}
	if value == missing && v.object("missing")[key] != nil {
		value += " — " + v.object("missing").label(key)
	}
	return value
}
func deviceMeasurementFor(c *ui.Context, v deviceObject, key string, capacity bool) string {
	missing := tr(c, "device.missing")
	value := v.valueWithMissing(key, missing)
	if capacity {
		value = deviceBytesWithMissing(v[key], missing)
	}
	if value == missing && v.object("missing")[key] != nil {
		value += " — " + v.object("missing").labelFor(c, key)
	}
	return value
}
func deviceRow(c *ui.Context, label, value string) {
	t := tokens(c)
	ariaLabel := trf(c, "device.row.aria_label", "label", label, "value", value)
	if compactLayout(c) {
		ui.Column(c).Gap(t.Density.Tight).Children(func() {
			ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			ui.Text(c, value).FontFeatures("tnum").Selectable().Label(ariaLabel)
		})
		return
	}
	ui.Row(c).AlignItems(ui.Start).Gap(t.Density.Section).Padding(t.Density.Hairline, 0).Children(func() {
		ui.Box(c).Width(c.Theme().Rem(10)).Shrink(0).Padding(2, 0, 0, 0).Children(func() {
			ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		ui.Text(c, value).Grow(1).MinWidth(0).FontFeatures("tnum").Selectable().Label(ariaLabel)
	})
}
func (s *viewState) refreshDevice() {
	if s.simulate(previewRefreshDevice) {
		return
	}
	s.workAs("refresh-device", func() (string, *core.ApiError) { return l10n.TR("device.action.refreshed"), s.service.RefreshDevice() })
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

func deviceHardware(c *ui.Context, hw deviceObject) {
	card(c, tr(c, "device.hardware.title"), func() {
		deviceRow(c, tr(c, "device.hardware.cpu"), trf(c, "device.hardware.cpu_value", "model", deviceMeasurementFor(c, hw, "cpu_model", false), "cores", deviceMeasurementFor(c, hw, "logical_cpus", false)))
		memory := trf(c, "device.hardware.memory_value", "total", deviceMeasurementFor(c, hw, "memory_total_bytes", true), "available", deviceMeasurementFor(c, hw, "memory_available_bytes", true))
		deviceRow(c, tr(c, "device.hardware.memory"), memory)
		gpu := hw.object("gpu")
		devices := gpu.list("devices")
		for i, item := range devices {
			v := deviceItem(item)
			name := v.valueFor(c, "name")
			if arch := strings.TrimSpace(fmt.Sprint(v["arch_family"])); v["arch_family"] != nil && arch != "" {
				name += " · " + arch
			}
			if v["cores"] != nil {
				name += " · " + trf(c, "device.hardware.gpu_cores", "cores", v.valueFor(c, "cores"))
			}
			if v.flag("metal_supported") {
				name += " · " + tr(c, "device.hardware.metal_supported")
			}
			if drv := strings.TrimSpace(fmt.Sprint(v["driver_version"])); v["driver_version"] != nil && drv != "" {
				name += " · " + trf(c, "device.hardware.driver_version", "version", drv)
			}
			label := "GPU"
			if len(devices) > 1 {
				label = fmt.Sprintf("GPU %d", i+1)
			}
			deviceRow(c, label, name)
			deviceRow(c, trf(c, "device.hardware.gpu_vram", "label", label), deviceMeasurementFor(c, v, "memory_total_bytes", true))
		}
		if len(devices) == 0 {
			value := tr(c, "device.missing")
			if gpu["reason"] != nil {
				value += " — " + gpu.labelFor(c, "reason")
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
				deviceRow(c, tr(c, "device.hardware.instruction_set"), strings.Join(flags, " · "))
			}
		}
		deviceRow(c, tr(c, "device.hardware.disk_free"), deviceMeasurementFor(c, hw, "disk_free_bytes", true))
		power := deviceMeasurementFor(c, hw, "ac_online", false)
		if online, ok := hw["ac_online"].(bool); ok {
			power = tr(c, "device.hardware.power_battery")
			if online {
				power = tr(c, "device.hardware.power_ac")
			}
		}
		deviceRow(c, tr(c, "device.hardware.power"), power)
		architecture := hw.valueFor(c, "architecture")
		if hw.flag("apple_silicon") {
			architecture = trf(c, "device.hardware.architecture_apple_silicon", "arch", architecture)
		}
		deviceRow(c, tr(c, "device.hardware.architecture"), architecture)
	})
}
