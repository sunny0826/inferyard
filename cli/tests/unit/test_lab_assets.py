"""Native Windows file identity guard refuses path swaps and NTFS reparse points."""

from inferyard.platforms.lab_identity import LabFileIdentity
from inferyard.platforms.windows_lab_api import override_windows_lab_api
from tests.unit.windows_lab_files_helpers import STAMP, Store


def test_file_guard_uses_native_identity_and_refuses_reparse():
    store = Store()
    volume, identifier, attributes, size, creation, write, change = STAMP
    identity = LabFileIdentity(
        r"C:\lab\model.gguf",
        "a" * 64,
        size,
        volume,
        int.from_bytes(identifier, "big"),
        write * 100,
        [volume, identifier.hex(), attributes, size, creation, write, change],
        "model",
    )
    with override_windows_lab_api(store):
        assert identity.unchanged()
        store.change_path_at = store.path_reads + 1
        assert not identity.unchanged()
        store.change_path_at = None
        store.reparse.add(r"C:\lab")
        assert not identity.unchanged()
