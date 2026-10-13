"""A clearly synthetic next-version wheel, exclusively for uv upgrade/rollback mechanics."""

import base64
import csv
import hashlib
import io
import zipfile
from email.parser import BytesParser


def synthetic_version(version):
    major, minor, patch = version.split(".")
    return f"{major}.{minor}.{int(patch) + 1}.dev0"


def make_upgrade_fixture(wheel, out):
    with zipfile.ZipFile(wheel) as source:
        metadata = next(n for n in source.namelist() if n.endswith(".dist-info/METADATA"))
        old = BytesParser().parsebytes(source.read(metadata))["Version"]
    new = synthetic_version(old)
    target = out / wheel.name.replace(old, new)
    records = []
    with zipfile.ZipFile(wheel) as source, zipfile.ZipFile(target, "x") as destination:
        for name in source.namelist():
            if name.endswith("/RECORD"):
                continue
            data = source.read(name)
            if name.endswith("/METADATA") or name == "inferyard/__init__.py":
                data = data.replace(old.encode(), new.encode())
            name = name.replace(f"-{old}.dist-info/", f"-{new}.dist-info/")
            destination.writestr(name, data)
            digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b"=")
            records.append((name, "sha256=" + digest.decode(), len(data)))
        record = f"inferyard-{new}.dist-info/RECORD"
        content = io.StringIO(newline="")
        csv.writer(content).writerows([*records, (record, "", "")])
        destination.writestr(record, content.getvalue())
    return target
