"""Stage46 文件夹具常量。导入本模块不加载 WinDLL，也不创建 Windows 目录。"""

from __future__ import annotations

FILE_ROOT = "D:\\lab46-source-host\\files"
DIAGNOSTIC_ROOT = "D:\\lab46-source-host"
EVIDENCE_ROOT = "D:\\inferyard\\artifacts\\ninfer-windows-20261005-stage46-source-host"
EXPERIMENT_STAGE = 46
LIBRARY_STAGE = 45
FILE_LIMIT = 8192
STREAM_LIMIT = 32768
MODES = frozenset({"file_io", "blocked_read", "output", "descendant"})
FRAME_FIELDS = (
    "experiment_stage",
    "mode",
    "protocol",
    "host_id",
    "execution_sha256",
    "scripts_sha256",
    "stage",
    "job_id",
    "status",
    "injected",
    "result",
)
READY_FIELDS = (
    "experiment_stage",
    "mode",
    "protocol",
    "host_id",
    "execution_sha256",
    "scripts_sha256",
    "stage",
    "job_id",
    "status",
)
OUTPUT_PREFIX = b"stage46-output-prefix\n"
OUTPUT_BODY = b"stage46-distinct-output-byte\n"
DESCENDANT_ARGV = (
    "python",
    "-c",
    "raise SystemExit(0)",
)
GENERIC_ACCESS = 0x10000000
FILE_SHARE_READ = 0x00000001
FILE_SHARE_WRITE = 0x00000002
CREATE_NEW = 1
OPEN_EXISTING = 3
FILE_ATTRIBUTE_NORMAL = 0x00000080
FILE_FLAG_OPEN_REPARSE_POINT = 0x00200000
FILE_FLAG_BACKUP_SEMANTICS = 0x02000000
FILE_READ_ATTRIBUTES = 0x00000080
FILE_READ_DATA = 0x00000001
FILE_WRITE_DATA = 0x00000002
INVALID_FILE_ATTRIBUTES = 0xFFFFFFFF
FILE_ATTRIBUTE_REPARSE_POINT = 0x00000400
ERROR_FILE_EXISTS = 80
ERROR_ALREADY_EXISTS = 183
ERROR_FILE_NOT_FOUND = 2
ERROR_PATH_NOT_FOUND = 3
ERROR_ACCESS_DENIED = 5
JOB_OBJECT_LIMIT_ACTIVE_PROCESS = 0x00000008
ACTIVE_PROCESS_LIMIT = 1
CREATE_SUSPENDED = 0x00000004
CREATE_BREAKAWAY_FROM_JOB = 0x01000000
JOB_OBJECT_LIMIT_BREAKAWAY_OK = 0x00000800
JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK = 0x00001000
