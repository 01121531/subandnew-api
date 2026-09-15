"""Only ship the raster decoders and Windows display plugins used by this app."""

from pathlib import Path

from PyInstaller.utils.hooks.qt import add_qt6_dependencies

hiddenimports, binaries, datas = add_qt6_dependencies(__file__)
allowed = {"qjpeg.dll", "qwebp.dll", "qwindows.dll", "qoffscreen.dll", "qminimal.dll"}
binaries = [item for item in binaries if Path(item[0]).name.lower() in allowed]
