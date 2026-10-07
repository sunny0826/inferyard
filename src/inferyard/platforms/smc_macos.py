"""Read-only AppleSMC RPC; private ABI, never writes a key or requests privilege.

Protocol reference: github.com/vladkens/macmon/blob/main/src_lib/sources.rs.
Only key enumeration (8), metadata (9), and value reads (5) are exposed.
"""

import ctypes as c
import sys
import weakref


class SMCError(ValueError):
    pass


class KeyInfo(c.Structure):
    _fields_ = [("size", c.c_uint32), ("kind", c.c_uint32), ("attributes", c.c_uint8)]


class Version(c.Structure):
    _fields_ = [
        ("major", c.c_uint8),
        ("minor", c.c_uint8),
        ("build", c.c_uint8),
        ("reserved", c.c_uint8),
        ("release", c.c_uint16),
    ]


class Limits(c.Structure):
    _fields_ = [
        ("version", c.c_uint16),
        ("length", c.c_uint16),
        ("cpu", c.c_uint32),
        ("gpu", c.c_uint32),
        ("memory", c.c_uint32),
    ]


class KeyData(c.Structure):
    _fields_ = [
        ("key", c.c_uint32),
        ("version", Version),
        ("limits", Limits),
        ("info", KeyInfo),
        ("result", c.c_uint8),
        ("status", c.c_uint8),
        ("command", c.c_uint8),
        ("index", c.c_uint32),
        ("data", c.c_uint8 * 32),
    ]


class SMC:
    def __init__(self):
        if sys.platform != "darwin" or c.sizeof(KeyData) != 80:
            raise SMCError("smc_native_abi_unavailable")
        io = c.CDLL("/System/Library/Frameworks/IOKit.framework/IOKit")
        signatures = {
            "IOServiceMatching": ([c.c_char_p], c.c_void_p),
            "IOServiceGetMatchingService": ([c.c_uint32, c.c_void_p], c.c_uint32),
            "IOServiceOpen": ([c.c_uint32, c.c_uint32, c.c_uint32, c.POINTER(c.c_uint32)], c.c_int),
            "IOServiceClose": ([c.c_uint32], c.c_int),
            "IOObjectRelease": ([c.c_uint32], c.c_int),
            "IORegistryEntryGetRegistryEntryID": ([c.c_uint32, c.POINTER(c.c_uint64)], c.c_int),
            "IOConnectCallStructMethod": (
                [c.c_uint32, c.c_uint32, c.c_void_p, c.c_size_t, c.c_void_p, c.POINTER(c.c_size_t)],
                c.c_int,
            ),
        }
        for name, (args, result) in signatures.items():
            function = getattr(io, name)
            function.argtypes, function.restype = args, result
        service = io.IOServiceGetMatchingService(0, io.IOServiceMatching(b"AppleSMC"))
        if not service:
            raise SMCError("smc_service_unavailable")
        connection = c.c_uint32()
        try:
            task = c.c_uint32.in_dll(c.CDLL(None), "mach_task_self_").value
            status = io.IOServiceOpen(service, task, 0, c.byref(connection))
            if status or not connection.value:
                raise SMCError("smc_connection_unavailable")
            self.io, self.service, self.connection = io, service, connection.value
            self.entry_id = self.identity()
        except BaseException:
            if connection.value:
                io.IOServiceClose(connection.value)
            io.IOObjectRelease(service)
            raise
        self.cleanup = weakref.finalize(self, self._close, io, self.connection, service)

    @staticmethod
    def _close(io, connection, service):
        io.IOServiceClose(connection)
        io.IOObjectRelease(service)

    def close(self):
        self.cleanup()

    def identity(self):
        value = c.c_uint64()
        if (
            self.io.IORegistryEntryGetRegistryEntryID(self.service, c.byref(value))
            or not value.value
        ):
            raise SMCError("smc_identity_unavailable")
        return value.value

    def _rpc(self, request):
        if self.identity() != self.entry_id:
            raise SMCError("source_changed")
        response, size = KeyData(), c.c_size_t(c.sizeof(KeyData))
        code = self.io.IOConnectCallStructMethod(
            self.connection,
            2,
            c.byref(request),
            c.sizeof(request),
            c.byref(response),
            c.byref(size),
        )
        if code or response.result or size.value != c.sizeof(KeyData):
            raise SMCError("smc_read_unavailable")
        if self.identity() != self.entry_id:
            raise SMCError("source_changed")
        return response

    @staticmethod
    def _key(key):
        if not isinstance(key, str) or len(key) != 4 or not key.isascii():
            raise SMCError("invalid_smc_key")
        return int.from_bytes(key.encode("ascii"), "big")

    def info(self, key):
        request = KeyData(key=self._key(key), command=9)
        info = self._rpc(request).info
        return {
            "size": info.size,
            "type": info.kind.to_bytes(4, "big").decode("ascii"),
            "attributes": info.attributes,
        }

    def read(self, key):
        info = self.info(key)
        if not 0 < info["size"] <= 32:
            raise SMCError("invalid_smc_payload_size")
        request = KeyData(key=self._key(key), command=5)
        request.info = KeyInfo(info["size"], self._key(info["type"]), info["attributes"])
        raw = bytes(self._rpc(request).data[: info["size"]])
        return info, raw

    def keys(self, *, limit=4096):
        info, raw = self.read("#KEY")
        if info["size"] != 4 or info["type"] != "ui32":
            raise SMCError("smc_key_inventory_invalid")
        count = int.from_bytes(raw, "big")
        if not 0 < count <= limit:
            raise SMCError("smc_key_inventory_limit")
        keys = [
            self._rpc(KeyData(command=8, index=i)).key.to_bytes(4, "big").decode("ascii")
            for i in range(count)
        ]
        if len(set(keys)) != count:
            raise SMCError("smc_key_inventory_duplicate")
        return keys
