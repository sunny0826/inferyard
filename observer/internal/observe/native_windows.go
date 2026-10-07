//go:build windows

package observe

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")
var user32 = syscall.NewLazyDLL("user32.dll")

type filetime struct{ Low, High uint32 }

func (f filetime) ticks() uint64 { return uint64(f.High)<<32 | uint64(f.Low) }

type memoryStatus struct {
	Length, Load                                                                         uint32
	Total, Available, PageTotal, PageAvailable, VirtualTotal, VirtualAvailable, Extended uint64
}
type memoryCounters struct {
	Size, Faults uint32
	Values       [9]uintptr
}
type processEntry struct {
	Size, Usage, PID        uint32
	Heap                    uintptr
	Module, Threads, Parent uint32
	Priority                int32
	Flags                   uint32
	Name                    [260]uint16
}
type displayDevice struct {
	Size        uint32
	Name        [32]uint16
	Description [128]uint16
	Flags       uint32
	ID, Key     [128]uint16
}
type versionInfo struct {
	Size, Major, Minor, Build, Platform uint32
	Service                             [128]uint16
	Extra                               [8]byte
}

// These layouts are deliberately limited to the supported 64-bit targets.
var _ [80 - unsafe.Sizeof(memoryCounters{})]byte
var _ [unsafe.Sizeof(memoryCounters{}) - 80]byte
var _ [568 - unsafe.Sizeof(processEntry{})]byte
var _ [unsafe.Sizeof(processEntry{}) - 568]byte

func windowsError(err error) error {
	if err == syscall.ERROR_ACCESS_DENIED {
		return ErrPermission
	}
	if err == syscall.Errno(87) {
		return ErrExited
	}
	return ErrUnavailable
}

func NativeProcess(pid int) (RawProcess, error) {
	var raw RawProcess
	h, _, err := kernel.NewProc("OpenProcess").Call(0x1000, 0, uintptr(pid))
	if h == 0 {
		return raw, windowsError(err)
	}
	defer kernel.NewProc("CloseHandle").Call(h)
	var create, exit, kern, user filetime
	ok, _, err := kernel.NewProc("GetProcessTimes").Call(h, uintptr(unsafe.Pointer(&create)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kern)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		return raw, windowsError(err)
	}
	if exit.ticks() != 0 {
		return raw, ErrExited
	}
	var name [1024]uint16
	size := uint32(len(name))
	ok, _, _ = kernel.NewProc("QueryFullProcessImageNameW").Call(h, 0, uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&size)))
	processName := ""
	if ok != 0 {
		processName = filepath.Base(syscall.UTF16ToString(name[:size]))
	}
	raw.Target = Target{pid, create.ticks(), processName}
	raw.CPUNS = Missing[uint64]("cpu_counter_invalid", "windows.GetProcessTimes.100ns")
	u, k := user.ticks(), kern.ticks()
	if u <= math.MaxUint64-k && u+k <= math.MaxUint64/100 {
		raw.CPUNS = Known((u+k)*100, "windows.GetProcessTimes.100ns")
	}
	var mem memoryCounters
	mem.Size = uint32(unsafe.Sizeof(mem))
	ok, _, err = kernel.NewProc("K32GetProcessMemoryInfo").Call(h, uintptr(unsafe.Pointer(&mem)), uintptr(mem.Size))
	raw.RSS = Missing[uint64]("working_set_unavailable", "windows.K32GetProcessMemoryInfo.WorkingSetSize")
	if ok != 0 {
		raw.RSS = Known(uint64(mem.Values[1]), "windows.K32GetProcessMemoryInfo.WorkingSetSize")
	} else if err == syscall.ERROR_ACCESS_DENIED {
		raw.RSS = Missing[uint64]("permission_denied", "windows.K32GetProcessMemoryInfo.WorkingSetSize")
	}
	// An object handle preserves identity; a final exit check rejects a dead target.
	var code uint32
	ok, _, err = kernel.NewProc("GetExitCodeProcess").Call(h, uintptr(unsafe.Pointer(&code)))
	if ok == 0 {
		return RawProcess{}, windowsError(err)
	}
	if code != 259 {
		return RawProcess{}, ErrExited
	}
	return raw, nil
}

func windowsMemory() (memoryStatus, bool) {
	var m memoryStatus
	m.Length = uint32(unsafe.Sizeof(m))
	ok, _, _ := kernel.NewProc("GlobalMemoryStatusEx").Call(uintptr(unsafe.Pointer(&m)))
	return m, ok != 0
}

func NativeHost(path string) HostMetrics {
	h := HostMetrics{MemoryAvailable: Missing[uint64]("memory_unavailable", "windows.GlobalMemoryStatusEx.AvailPhys"),
		DiskAvailable: Missing[uint64]("disk_unavailable", "windows.GetDiskFreeSpaceEx.available_to_caller")}
	if m, ok := windowsMemory(); ok {
		h.MemoryAvailable = Known(m.Available, "windows.GlobalMemoryStatusEx.AvailPhys")
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return h
	}
	var free, total, totalFree uint64
	ok, _, _ := kernel.NewProc("GetDiskFreeSpaceExW").Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&totalFree)))
	if ok != 0 {
		h.DiskAvailable = Known(free, "windows.GetDiskFreeSpaceEx.available_to_caller")
	}
	return h
}

func NativeInventory() Inventory {
	i := InventoryBase()
	i.MemoryTotal = Missing[uint64]("memory_unavailable", "windows.GlobalMemoryStatusEx.TotalPhys")
	if m, ok := windowsMemory(); ok {
		i.MemoryTotal = Known(m.Total, "windows.GlobalMemoryStatusEx.TotalPhys")
	}
	i.OSVersion = Missing[string]("os_version_unavailable", "windows.RtlGetVersion")
	var v versionInfo
	v.Size = uint32(unsafe.Sizeof(v))
	status, _, _ := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion").Call(uintptr(unsafe.Pointer(&v)))
	if status == 0 {
		i.OSVersion = Known(fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Build), "windows.RtlGetVersion")
	}
	i.CPUModel = Missing[string]("cpu_model_unavailable", "windows.registry.ProcessorNameString")
	var key syscall.Handle
	path, _ := syscall.UTF16PtrFromString(`HARDWARE\DESCRIPTION\System\CentralProcessor\0`)
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, path, 0, syscall.KEY_READ, &key) == nil {
		defer syscall.RegCloseKey(key)
		var data [512]uint16
		size := uint32(unsafe.Sizeof(data))
		var kind uint32
		name, _ := syscall.UTF16PtrFromString("ProcessorNameString")
		if syscall.RegQueryValueEx(key, name, nil, &kind, (*byte)(unsafe.Pointer(&data[0])), &size) == nil && kind == syscall.REG_SZ {
			if model := strings.TrimSpace(syscall.UTF16ToString(data[:])); model != "" {
				i.CPUModel = Known(model, "windows.registry.ProcessorNameString")
			}
		}
	}
	i.Graphics = Missing[[]string]("graphics_inventory_unavailable", "windows.EnumDisplayDevicesW")
	names := []string{}
	for index := 0; index < 16; index++ {
		var d displayDevice
		d.Size = uint32(unsafe.Sizeof(d))
		ok, _, _ := user32.NewProc("EnumDisplayDevicesW").Call(0, uintptr(index), uintptr(unsafe.Pointer(&d)), 0)
		if ok == 0 {
			break
		}
		if name := strings.TrimSpace(syscall.UTF16ToString(d.Description[:])); name != "" {
			names = append(names, name)
		}
	}
	if len(names) > 0 {
		i.Graphics = Known(names, "windows.EnumDisplayDevicesW")
	}
	return i
}

func NativeDiscover() ([]Target, error) {
	h, _, err := kernel.NewProc("CreateToolhelp32Snapshot").Call(2, 0)
	if h == ^uintptr(0) {
		return nil, windowsError(err)
	}
	defer kernel.NewProc("CloseHandle").Call(h)
	var entry processEntry
	entry.Size = uint32(unsafe.Sizeof(entry))
	ok, _, err := kernel.NewProc("Process32FirstW").Call(h, uintptr(unsafe.Pointer(&entry)))
	result := []Target{}
	for ok != 0 {
		if IsCandidate(syscall.UTF16ToString(entry.Name[:])) {
			p, e := NativeProcess(int(entry.PID))
			if e == nil {
				result = append(result, p.Target)
			}
		}
		if len(result) >= 32 {
			return result, ErrUnavailable
		}
		ok, _, err = kernel.NewProc("Process32NextW").Call(h, uintptr(unsafe.Pointer(&entry)))
	}
	if err != syscall.ERROR_NO_MORE_FILES {
		return result, ErrUnavailable
	}
	return result, nil
}
