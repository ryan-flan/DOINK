import os
import unittest
from pathlib import Path

from doink.config import native_path


@unittest.skipUnless(os.name == "posix", "WSL path translation")
class NativePathTest(unittest.TestCase):
    def test_windows_path_maps_to_mnt(self):
        self.assertEqual(
            native_path(r"C:\Program Files (x86)\World of Warcraft\_classic_beta_\WTF\DOINK.lua"),
            Path("/mnt/c/Program Files (x86)/World of Warcraft/_classic_beta_/WTF/DOINK.lua"))

    def test_forward_slashes_and_other_drives(self):
        self.assertEqual(native_path("D:/Games/WoW/DOINK.lua"), Path("/mnt/d/Games/WoW/DOINK.lua"))

    def test_posix_path_untouched(self):
        self.assertEqual(native_path("/mnt/c/x/DOINK.lua"), Path("/mnt/c/x/DOINK.lua"))


if __name__ == "__main__":
    unittest.main()
