from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from tools.release_manifest import build_release_manifest, main


class ReleaseManifestTest(unittest.TestCase):
    def test_build_release_manifest_lists_sorted_assets(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            assets = Path(tmp) / "assets"
            assets.mkdir()
            (assets / "inferyard-studio-v0.10.0-darwin-arm64.dmg").write_bytes(b"dmg-data")
            (assets / "SHA256SUMS").write_text("abc  inferyard-studio-v0.10.0-darwin-arm64.dmg\n", encoding="utf-8")

            manifest = build_release_manifest(
                tag="v0.10.0",
                repo="sunny0826/inferyard",
                notes="## Highlights\n\n- Fixed rate limit\n",
                assets_dir=assets,
                published_at="2026-11-25T12:00:00Z",
            )

            self.assertEqual(manifest["tag_name"], "v0.10.0")
            self.assertEqual(manifest["name"], "v0.10.0")
            self.assertEqual(manifest["body"], "## Highlights\n\n- Fixed rate limit")
            self.assertEqual(
                manifest["html_url"],
                "https://github.com/sunny0826/inferyard/releases/tag/v0.10.0",
            )
            self.assertFalse(manifest["draft"])
            self.assertFalse(manifest["prerelease"])
            self.assertEqual(manifest["published_at"], "2026-11-25T12:00:00Z")
            self.assertEqual(
                manifest["assets"],
                [
                    {
                        "name": "SHA256SUMS",
                        "browser_download_url": "https://github.com/sunny0826/inferyard/releases/download/v0.10.0/SHA256SUMS",
                        "size": len("abc  inferyard-studio-v0.10.0-darwin-arm64.dmg\n"),
                    },
                    {
                        "name": "inferyard-studio-v0.10.0-darwin-arm64.dmg",
                        "browser_download_url": "https://github.com/sunny0826/inferyard/releases/download/v0.10.0/inferyard-studio-v0.10.0-darwin-arm64.dmg",
                        "size": 8,
                    },
                ],
            )

    def test_build_release_manifest_requires_checksums(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            assets = Path(tmp) / "assets"
            assets.mkdir()
            (assets / "inferyard-studio-v0.10.0-linux-amd64.tar.gz").write_bytes(b"tar")

            with self.assertRaisesRegex(ValueError, "SHA256SUMS"):
                build_release_manifest(
                    tag="v0.10.0",
                    repo="sunny0826/inferyard",
                    notes="",
                    assets_dir=assets,
                )

    def test_cli_writes_json_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            assets = root / "assets"
            assets.mkdir()
            (assets / "SHA256SUMS").write_text("deadbeef  file\n", encoding="utf-8")
            notes = root / "release-notes.md"
            notes.write_text("Release body\n", encoding="utf-8")
            out = root / "out" / "latest.json"

            code = main(
                [
                    "--tag",
                    "v0.10.1",
                    "--repo",
                    "sunny0826/inferyard",
                    "--notes-file",
                    str(notes),
                    "--assets-dir",
                    str(assets),
                    "--published-at",
                    "2026-11-25T12:00:00Z",
                    "--out",
                    str(out),
                ]
            )

            self.assertEqual(code, 0)
            payload = json.loads(out.read_text(encoding="utf-8"))
            self.assertEqual(payload["tag_name"], "v0.10.1")
            self.assertEqual(payload["body"], "Release body")


if __name__ == "__main__":
    unittest.main()
