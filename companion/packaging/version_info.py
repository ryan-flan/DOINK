"""Writes PyInstaller's Windows version resource for doink.exe, so
Properties > Details shows what the file is and which version.

    python packaging/version_info.py version_info.txt
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))
from doink import __version__  # noqa: E402

numbers = tuple(([int(p) for p in __version__.split(".")] + [0, 0, 0])[:4])

TEMPLATE = f"""VSVersionInfo(
  ffi=FixedFileInfo(filevers={numbers}, prodvers={numbers}, mask=0x3f, flags=0x0,
                    OS=0x40004, fileType=0x1, subtype=0x0, date=(0, 0)),
  kids=[
    StringFileInfo([StringTable('040904B0', [
      StringStruct('CompanyName', 'DOINK'),
      StringStruct('FileDescription', 'DOINK companion: posts WoW events to Discord'),
      StringStruct('FileVersion', '{__version__}'),
      StringStruct('InternalName', 'doink'),
      StringStruct('OriginalFilename', 'doink.exe'),
      StringStruct('ProductName', 'DOINK'),
      StringStruct('ProductVersion', '{__version__}'),
      StringStruct('Comments', 'Source and builds: github.com/ryan-flan/DOINK')])]),
    VarFileInfo([VarStruct('Translation', [1033, 1200])])
  ]
)
"""

Path(sys.argv[1]).write_text(TEMPLATE, encoding="utf-8")
