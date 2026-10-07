//go:build darwin && cgo

package observe

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <libproc.h>
#include <sys/proc_info.h>
#include <sys/sysctl.h>
#include <sys/statvfs.h>
#include <mach/mach.h>
#include <mach/mach_time.h>
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <errno.h>
#include <stdlib.h>
#include <string.h>

typedef struct { uint64_t start, cpu, rss; char name[128]; } lab_proc;
static int lab_read_process(int pid, lab_proc *out) {
    struct proc_taskallinfo info = {0};
    struct proc_bsdinfo after = {0};
    errno = 0;
    if (proc_pidinfo(pid, PROC_PIDTASKALLINFO, 0, &info, sizeof(info)) != sizeof(info))
        return errno ? errno : EIO;
    proc_name(pid, out->name, sizeof(out->name));
    if (proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &after, sizeof(after)) != sizeof(after))
        return errno ? errno : EIO;
    if (info.pbsd.pbi_start_tvsec != after.pbi_start_tvsec ||
        info.pbsd.pbi_start_tvusec != after.pbi_start_tvusec) return EIO;
    if (after.pbi_status == 5) return ESRCH;
    out->start = after.pbi_start_tvsec * 1000000ULL + after.pbi_start_tvusec;
    mach_timebase_info_data_t scale = {0};
    if (mach_timebase_info(&scale) != KERN_SUCCESS || !scale.denom) return EIO;
    __uint128_t cpu = ((__uint128_t)info.ptinfo.pti_total_user + info.ptinfo.pti_total_system)
        * scale.numer / scale.denom;
    if (cpu > UINT64_MAX) return EIO;
    out->cpu = (uint64_t)cpu;
    out->rss = info.ptinfo.pti_resident_size;
    return 0;
}
static int lab_memory(uint64_t *available) {
    vm_statistics64_data_t vm = {0};
    mach_msg_type_number_t count = HOST_VM_INFO64_COUNT;
    vm_size_t page = 0;
    mach_port_t host = mach_host_self();
    int ok = host_page_size(host, &page) == KERN_SUCCESS &&
        host_statistics64(host, HOST_VM_INFO64, (host_info64_t)&vm, &count) == KERN_SUCCESS;
    mach_port_deallocate(mach_task_self(), host);
    if (!ok || !page) return EIO;
    *available = ((uint64_t)vm.free_count + vm.inactive_count) * page;
    return 0;
}
static int lab_disk(const char *path, uint64_t *available) {
    struct statvfs stat = {0};
    if (statvfs(path, &stat) != 0) return errno ? errno : EIO;
    if (stat.f_frsize && (uint64_t)stat.f_bavail > UINT64_MAX / stat.f_frsize) return EIO;
    *available = (uint64_t)stat.f_bavail * stat.f_frsize;
    return 0;
}
static int lab_sys_string(const char *name, char *value, size_t capacity) {
    size_t size = capacity;
    if (sysctlbyname(name, value, &size, NULL, 0) || size == 0 || size > capacity || value[size-1] != 0)
        return EIO;
    if (strnlen(value, size) != size-1) return EIO;
    return 0;
}
static int lab_sys_u64(const char *name, uint64_t *value) {
    size_t size = sizeof(*value);
    return sysctlbyname(name, value, &size, NULL, 0) || size != sizeof(*value) ? EIO : 0;
}
static int lab_pids(int *pids, int size) { return proc_listallpids(pids, size); }
static void lab_name(int pid, char *name, int size) { proc_name(pid, name, size); }
static int lab_graphics(char *value, size_t capacity) {
    io_iterator_t iter = 0;
    if (IOServiceGetMatchingServices(kIOMainPortDefault, IOServiceMatching("IOAccelerator"), &iter) != KERN_SUCCESS)
        return EIO;
    io_object_t entry;
    int found = 0;
    while ((entry = IOIteratorNext(iter))) {
        if (!found) {
            CFTypeRef property = IORegistryEntryCreateCFProperty(entry, CFSTR("IOClass"), kCFAllocatorDefault, 0);
            if (property && CFGetTypeID(property) == CFStringGetTypeID())
                found = CFStringGetCString(property, value, capacity, kCFStringEncodingUTF8);
            if (property) CFRelease(property);
        }
        IOObjectRelease(entry);
    }
    IOObjectRelease(iter);
    return found ? 0 : EIO;
}
*/
import "C"

import (
	"strings"
	"unsafe"
)

func darwinError(code C.int) error {
	if code == C.ESRCH {
		return ErrExited
	}
	if code == C.EPERM || code == C.EACCES {
		return ErrPermission
	}
	return ErrUnavailable
}

func NativeProcess(pid int) (RawProcess, error) {
	var p C.lab_proc
	var raw RawProcess
	if err := C.lab_read_process(C.int(pid), &p); err != 0 {
		return raw, darwinError(err)
	}
	raw.Target = Target{pid, uint64(p.start), C.GoString(&p.name[0])}
	raw.CPUNS = Known(uint64(p.cpu), "darwin.libproc.taskallinfo.mach_timebase_ns")
	raw.RSS = Known(uint64(p.rss), "darwin.libproc.taskallinfo.resident_size")
	return raw, nil
}

func NativeHost(path string) HostMetrics {
	h := HostMetrics{MemoryAvailable: Missing[uint64]("memory_unavailable", "darwin.mach.free_plus_inactive_pages"),
		DiskAvailable: Missing[uint64]("disk_unavailable", "darwin.statvfs.bavail")}
	var value C.uint64_t
	if C.lab_memory(&value) == 0 {
		h.MemoryAvailable = Known(uint64(value), "darwin.mach.free_plus_inactive_pages")
	}
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	if C.lab_disk(p, &value) == 0 {
		h.DiskAvailable = Known(uint64(value), "darwin.statvfs.bavail")
	}
	return h
}

func darwinString(key string) Metric[string] {
	name := C.CString(key)
	defer C.free(unsafe.Pointer(name))
	var value [4096]C.char
	if C.lab_sys_string(name, &value[0], C.size_t(len(value))) != 0 {
		return Missing[string]("native_string_unavailable", "darwin.sysctl."+key)
	}
	text := strings.TrimSpace(C.GoString(&value[0]))
	if text == "" {
		return Missing[string]("native_string_empty", "darwin.sysctl."+key)
	}
	return Known(text, "darwin.sysctl."+key)
}

func NativeInventory() Inventory {
	i := InventoryBase()
	i.OSVersion = darwinString("kern.osproductversion")
	i.CPUModel = darwinString("machdep.cpu.brand_string")
	name := C.CString("hw.memsize")
	defer C.free(unsafe.Pointer(name))
	var mem C.uint64_t
	i.MemoryTotal = Missing[uint64]("memory_unavailable", "darwin.sysctl.hw.memsize")
	if C.lab_sys_u64(name, &mem) == 0 {
		i.MemoryTotal = Known(uint64(mem), "darwin.sysctl.hw.memsize")
	}
	var graphics [256]C.char
	i.Graphics = Missing[[]string]("graphics_inventory_unavailable", "darwin.iokit.IOAccelerator.IOClass")
	if C.lab_graphics(&graphics[0], 256) == 0 {
		i.Graphics = Known([]string{C.GoString(&graphics[0])}, "darwin.iokit.IOAccelerator.IOClass")
	}
	i.Limitations = append(i.Limitations, "unified_memory_not_dedicated_gpu_memory", "available_is_free_plus_inactive_estimate")
	return i
}

func NativeDiscover() ([]Target, error) {
	var pids [32768]C.int
	n := int(C.lab_pids(&pids[0], C.int(unsafe.Sizeof(pids))))
	if n <= 0 || n >= len(pids) {
		return nil, ErrUnavailable
	}
	result := []Target{}
	for _, pid := range pids[:n] {
		if pid <= 0 {
			continue
		}
		var name [128]C.char
		C.lab_name(pid, &name[0], 128)
		if !IsCandidate(C.GoString(&name[0])) {
			continue
		}
		p, err := NativeProcess(int(pid))
		if err == nil {
			result = append(result, p.Target)
		}
		if len(result) >= 32 {
			return result, ErrUnavailable
		}
	}
	return result, nil
}
