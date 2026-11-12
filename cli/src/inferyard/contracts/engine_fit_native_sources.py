"""Immutable provenance labels for Windows diagnostic observations."""

WINDOWS_START_SOURCE = "GetProcessTimes:creation_FILETIME_100ns_since_1601"
WINDOWS_LISTENER_SOURCE = "GetExtendedTcpTable:owner_pid"
WINDOWS_SCOPE = {
    "processes": "bound_pid_and_observed_live_descendants_via_psutil_Process_children",
    "rss": "psutil:Windows:Process.memory_info:working_set_bytes; "
    "summed_shared_pages_may_be_counted_more_than_once",
    "cpu": "psutil:Windows:Process.cpu_times:user+system_seconds; "
    "cumulative_live_processes_excludes_exited_children",
    "memory_available": "GlobalMemoryStatusEx:ullAvailPhys; host_not_model_attribution",
    "process_start": WINDOWS_START_SOURCE,
}
WINDOWS_LIMITATIONS = (
    "model_binding_is_startup_file_not_observed_gpu_residency",
    "windows_cpu_temperature_not_collected",
    "windows_executable_hash_is_current_disk_file_not_loaded_image_bytes",
    "windows_gpu_backend_and_model_residency_not_independently_verified",
)
