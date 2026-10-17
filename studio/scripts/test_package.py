import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from package import package, require_release_build


class ReleasePackagingTests(unittest.TestCase):
    def test_build_tags(self):
        for tags, accepted in [("release", True), ("ui_debug,release", True),
                               ("ui_debug", False), ("", False)]:
            with self.subTest(tags=tags):
                output = json.dumps({"Settings": [{"Key": "-tags", "Value": tags}]})
                result = subprocess.CompletedProcess([], 0, stdout=output)
                with patch("package.subprocess.run", return_value=result):
                    if accepted:
                        require_release_build(Path("app"))
                    else:
                        with self.assertRaises(ValueError):
                            require_release_build(Path("app"))

    def test_missing_build_information_is_rejected(self):
        for output in ["{}", "not build info"]:
            result = subprocess.CompletedProcess([], 0, stdout=output)
            with patch("package.subprocess.run", return_value=result):
                with self.assertRaises(ValueError):
                    require_release_build(Path("app"))

    def test_all_inputs_are_checked_before_writing_distributions(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with patch("package.require_release_build", side_effect=[None, ValueError("debug")]):
                with self.assertRaises(ValueError):
                    package(root)
            self.assertEqual(list(root.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
