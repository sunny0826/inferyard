"""Stage46 WinAPI 结构。只定义布局，不加载 WinDLL。"""

from __future__ import annotations


def command_line(argv):
    return " ".join(_quote(arg) for arg in argv)


def startup_info(ctypes):
    class StartupInfo(ctypes.Structure):
        _fields_ = [
            ("cb", ctypes.c_uint32),
            ("lpReserved", ctypes.c_wchar_p),
            ("lpDesktop", ctypes.c_wchar_p),
            ("lpTitle", ctypes.c_wchar_p),
            ("dwX", ctypes.c_uint32),
            ("dwY", ctypes.c_uint32),
            ("dwXSize", ctypes.c_uint32),
            ("dwYSize", ctypes.c_uint32),
            ("dwXCountChars", ctypes.c_uint32),
            ("dwYCountChars", ctypes.c_uint32),
            ("dwFillAttribute", ctypes.c_uint32),
            ("dwFlags", ctypes.c_uint32),
            ("wShowWindow", ctypes.c_uint16),
            ("cbReserved2", ctypes.c_uint16),
            ("lpReserved2", ctypes.c_void_p),
            ("hStdInput", ctypes.c_void_p),
            ("hStdOutput", ctypes.c_void_p),
            ("hStdError", ctypes.c_void_p),
        ]

    info = StartupInfo()
    info.cb = ctypes.sizeof(info)
    return info


def process_information(ctypes):
    class ProcessInformation(ctypes.Structure):
        _fields_ = [
            ("hProcess", ctypes.c_void_p),
            ("hThread", ctypes.c_void_p),
            ("dwProcessId", ctypes.c_uint32),
            ("dwThreadId", ctypes.c_uint32),
        ]

    return ProcessInformation()


def basic_limits(ctypes):
    dword = ctypes.c_uint32

    class BasicLimits(ctypes.Structure):
        _fields_ = [
            ("PerProcessUserTimeLimit", ctypes.c_int64),
            ("PerJobUserTimeLimit", ctypes.c_int64),
            ("LimitFlags", dword),
            ("_pad_flags", dword),
            ("MinimumWorkingSetSize", ctypes.c_size_t),
            ("MaximumWorkingSetSize", ctypes.c_size_t),
            ("ActiveProcessLimit", dword),
            ("_pad_limit", dword),
            ("Affinity", ctypes.c_size_t),
            ("PriorityClass", dword),
            ("SchedulingClass", dword),
        ]

    return BasicLimits()


def extended_limits(ctypes):
    class Extended(ctypes.Structure):
        _fields_ = [
            ("BasicLimitInformation", type(basic_limits(ctypes))),
            ("IoInfo", ctypes.c_uint64 * 6),
            ("ProcessMemoryLimit", ctypes.c_size_t),
            ("JobMemoryLimit", ctypes.c_size_t),
            ("PeakProcessMemoryUsed", ctypes.c_size_t),
            ("PeakJobMemoryUsed", ctypes.c_size_t),
        ]

    return Extended()


def basic_accounting(ctypes):
    class Accounting(ctypes.Structure):
        _fields_ = [
            ("TotalUserTime", ctypes.c_int64),
            ("TotalKernelTime", ctypes.c_int64),
            ("ThisPeriodTotalUserTime", ctypes.c_int64),
            ("ThisPeriodTotalKernelTime", ctypes.c_int64),
            ("TotalPageFaultCount", ctypes.c_uint32),
            ("TotalProcesses", ctypes.c_uint32),
            ("ActiveProcesses", ctypes.c_uint32),
            ("TotalTerminatedProcesses", ctypes.c_uint32),
        ]

    return Accounting()


def _quote(arg):
    if arg == "":
        return '""'
    if not any(char in arg for char in ' \t"'):
        return arg
    quoted = ['"']
    slashes = 0
    for char in arg:
        if char == "\\":
            slashes += 1
            continue
        if char == '"':
            quoted.append("\\" * (slashes * 2 + 1))
            quoted.append('"')
        else:
            if slashes:
                quoted.append("\\" * slashes)
            quoted.append(char)
        slashes = 0
    if slashes:
        quoted.append("\\" * (slashes * 2))
    quoted.append('"')
    return "".join(quoted)
