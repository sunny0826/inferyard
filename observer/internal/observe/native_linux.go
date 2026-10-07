//go:build linux

package observe

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

func readSmall(path string, limit int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return "", ErrUnavailable
	}
	return string(b), nil
}

func nativeError(err error) error {
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return ErrExited
	}
	if errors.Is(err, os.ErrPermission) {
		return ErrPermission
	}
	return ErrUnavailable
}

func linuxHZ() uint64 {
	data, err := readSmall("/proc/self/auxv", 8192)
	if err != nil {
		return 0
	}
	b := []byte(data)
	for len(b) >= 16 {
		key := binary.LittleEndian.Uint64(b[:8])
		value := binary.LittleEndian.Uint64(b[8:16])
		if key == 17 {
			return value
		}
		b = b[16:]
	}
	return 0
}

var clockOnce sync.Once
var clockHZ uint64

func NativeProcess(pid int) (RawProcess, error) {
	clockOnce.Do(func() { clockHZ = linuxHZ() })
	var raw RawProcess
	root := filepath.Join("/proc", strconv.Itoa(pid))
	before, err := readSmall(filepath.Join(root, "stat"), 8192)
	if err != nil {
		return raw, nativeError(err)
	}
	first, err := parseProcStat(before)
	if err != nil {
		return raw, err
	}
	if first.zombie {
		return raw, ErrExited
	}
	status, statusErr := readSmall(filepath.Join(root, "status"), 65536)
	name, nameErr := readSmall(filepath.Join(root, "comm"), 256)
	if nameErr != nil {
		name = ""
	}
	after, err := readSmall(filepath.Join(root, "stat"), 8192)
	if err != nil {
		return raw, nativeError(err)
	}
	last, err := parseProcStat(after)
	if err != nil {
		return raw, err
	}
	if last.zombie {
		return raw, ErrExited
	}
	if first.start != last.start {
		return raw, ErrUnavailable
	}
	raw.Target = Target{pid, last.start, strings.TrimSpace(name)}
	raw.CPUNS = Missing[uint64]("clock_ticks_unavailable", "linux.proc.stat.AT_CLKTCK")
	if value, err := ticksNS(last.cpu, clockHZ); err == nil {
		raw.CPUNS = Known(value, "linux.proc.stat.AT_CLKTCK")
	}
	raw.RSS = Missing[uint64]("rss_unavailable", "linux.proc.status.VmRSS")
	if statusErr != nil {
		raw.RSS = Missing[uint64](ErrorReason(nativeError(statusErr)), "linux.proc.status.VmRSS")
	} else if value, err := parseMemValue(status, "VmRSS"); err == nil {
		raw.RSS = Known(value, "linux.proc.status.VmRSS")
	}
	return raw, nil
}

func NativeHost(path string) HostMetrics {
	h := HostMetrics{MemoryAvailable: Missing[uint64]("memory_unavailable", "linux.proc.meminfo.MemAvailable"),
		DiskAvailable: Missing[uint64]("disk_unavailable", "linux.statfs.bavail")}
	text, err := readSmall("/proc/meminfo", 65536)
	if err == nil {
		if v, err := parseMemValue(text, "MemAvailable"); err == nil {
			h.MemoryAvailable = Known(v, "linux.proc.meminfo.MemAvailable")
		}
	}
	var stat syscall.Statfs_t
	if syscall.Statfs(path, &stat) == nil && stat.Bsize > 0 && stat.Bavail <= math.MaxUint64/uint64(stat.Bsize) {
		h.DiskAvailable = Known(stat.Bavail*uint64(stat.Bsize), "linux.statfs.bavail")
	}
	return h
}

func NativeInventory() Inventory {
	i := InventoryBase()
	i.OSVersion = Missing[string]("os_version_unavailable", "linux.proc.osrelease")
	if v, e := readSmall("/proc/sys/kernel/osrelease", 256); e == nil {
		i.OSVersion = Known(strings.TrimSpace(v), "linux.proc.osrelease")
	}
	i.CPUModel = Missing[string]("cpu_model_unavailable", "linux.proc.cpuinfo")
	if text, e := readSmall("/proc/cpuinfo", 1024*1024); e == nil {
		for _, line := range strings.Split(text, "\n") {
			key, value, ok := strings.Cut(line, ":")
			if ok && (strings.TrimSpace(key) == "model name" || strings.TrimSpace(key) == "Hardware") {
				i.CPUModel = Known(strings.TrimSpace(value), "linux.proc.cpuinfo")
				break
			}
		}
	}
	i.MemoryTotal = Missing[uint64]("memory_unavailable", "linux.proc.meminfo.MemTotal")
	if text, e := readSmall("/proc/meminfo", 65536); e == nil {
		if v, e := parseMemValue(text, "MemTotal"); e == nil {
			i.MemoryTotal = Known(v, "linux.proc.meminfo.MemTotal")
		}
	}
	i.Graphics = Missing[[]string]("graphics_inventory_unavailable", "linux.sysfs.drm.pci_ids")
	devices := []string{}
	paths, _ := filepath.Glob("/sys/class/drm/card[0-9]*/device")
	for _, path := range paths {
		if len(devices) >= 16 {
			break
		}
		vendor, e1 := readSmall(filepath.Join(path, "vendor"), 128)
		device, e2 := readSmall(filepath.Join(path, "device"), 128)
		if e1 == nil && e2 == nil {
			devices = append(devices, strings.TrimSpace(vendor)+":"+strings.TrimSpace(device))
		}
	}
	if len(devices) > 0 {
		i.Graphics = Known(devices, "linux.sysfs.drm.pci_ids")
	}
	i.Limitations = append(i.Limitations, "pid_namespace_scope_and_cgroup_limits_not_analyzed")
	return i
}

func NativeDiscover() ([]Target, error) {
	directory, err := os.Open("/proc")
	if err != nil {
		return nil, ErrUnavailable
	}
	defer directory.Close()
	result := []Target{}
	visited := 0
	for {
		entries, err := directory.ReadDir(128)
		if err != nil && err != io.EOF {
			return result, ErrUnavailable
		}
		for _, entry := range entries {
			visited++
			if visited > 32768 {
				return result, ErrUnavailable
			}
			pid, e := strconv.Atoi(entry.Name())
			if e != nil || pid <= 0 {
				continue
			}
			name, e := readSmall(filepath.Join("/proc", entry.Name(), "comm"), 256)
			if e != nil || !IsCandidate(strings.TrimSpace(name)) {
				continue
			}
			p, e := NativeProcess(pid)
			if e == nil {
				result = append(result, p.Target)
			}
			if len(result) >= 32 {
				return result, ErrUnavailable
			}
		}
		if err == io.EOF {
			break
		}
	}
	return result, nil
}
