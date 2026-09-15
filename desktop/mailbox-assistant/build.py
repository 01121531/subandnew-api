"""Build a portable Windows distribution and its source/license companion."""

import hashlib
import importlib.metadata
import json
import os
import shutil
import subprocess
import sys
import tarfile
import urllib.request
import zipfile
from pathlib import Path

import PySide6

ROOT = Path(__file__).resolve().parent
REPO = ROOT.parent.parent
VERSION = (REPO / "VERSION").read_text().strip()
SOURCES = {
    "qtbase": "https://codeload.github.com/qt/qtbase/tar.gz/refs/tags/v6.11.2",
    "qtimageformats": "https://codeload.github.com/qt/qtimageformats/tar.gz/refs/tags/v6.11.2",
    "qtsvg": "https://codeload.github.com/qt/qtsvg/tar.gz/refs/tags/v6.11.2",
    "pyside": "https://codeload.github.com/qtproject/pyside-pyside-setup/tar.gz/refs/tags/v6.11.2",
}


def run(*args: str) -> None:
    env = dict(os.environ)
    # Avoid accidentally collecting DLLs from unrelated tools on the developer's PATH.
    env["PATH"] = os.pathsep.join(
        (
            str(Path(sys.executable).parent),
            str(Path(sys.base_prefix)),
            str(Path(os.environ["SystemRoot"]) / "System32"),
            os.environ["SystemRoot"],
        )
    )
    subprocess.run(args, cwd=ROOT, env=env, check=True)


def archive(source: Path, destination: Path, allowed_roots: set[str] | None = None) -> None:
    with zipfile.ZipFile(destination, "w", zipfile.ZIP_DEFLATED, compresslevel=6) as output:
        for file in sorted(source.rglob("*")):
            if allowed_roots is not None and file.relative_to(source).parts[0] not in allowed_roots:
                continue
            if file.is_file():
                output.write(file, Path(source.name) / file.relative_to(source))


def main() -> None:
    if sys.platform != "win32" or sys.maxsize <= 2**32:
        raise SystemExit("Windows x64 Python is required")
    if VERSION != "v" + importlib.metadata.version("mailbox-assistant"):
        raise SystemExit("Application and repository versions must match")
    work = ROOT / "build"
    work.mkdir(exist_ok=True)
    sources = work / "sources"
    sources.mkdir(exist_ok=True)
    license_dir = work / "LICENSES"
    license_dir.mkdir(exist_ok=True)
    provenance = {}
    for name, url in SOURCES.items():
        target = sources / f"{name}-6.11.2.tar.gz"
        if not target.exists():
            with urllib.request.urlopen(url, timeout=120) as response, target.open("wb") as out:
                shutil.copyfileobj(response, out)
        with target.open("rb") as stream:
            provenance[name] = {
                "source": url,
                "sha256": hashlib.file_digest(stream, "sha256").hexdigest(),
            }
        # Read license text only; never execute or extract paths from downloaded archives.
        with tarfile.open(target) as source:
            for member in source:
                path = Path(member.name)
                if not member.isfile() or member.size > 2 * 1024 * 1024:
                    continue
                upper = path.name.upper()
                if "LICENSES" not in path.parts and not upper.startswith(
                    ("LICENSE", "COPYING", "COPYRIGHT", "NOTICE")
                ):
                    continue
                stream = source.extractfile(member)
                if stream is not None:
                    key = hashlib.sha256(member.name.encode()).hexdigest()[:12]
                    (license_dir / f"{name}-{key}-{path.name}").write_bytes(stream.read())
    for name in ("PySide6", "PySide6_Essentials", "PySide6_Addons", "shiboken6", "pyinstaller"):
        dist = importlib.metadata.distribution(name)
        for file in dist.files or []:
            if "license" in str(file).lower() and ".dist-info/" in str(file).replace("\\", "/"):
                source = Path(dist.locate_file(file))
                if source.is_file():
                    shutil.copy2(source, license_dir / (name + "-" + source.name))
    python_license = Path(sys.base_prefix) / "LICENSE.txt"
    if not python_license.is_file():
        raise SystemExit("Python license missing")
    shutil.copy2(python_license, license_dir / "Python-LICENSE.txt")
    shutil.copy2(REPO / "LICENSE", license_dir / "MailboxAssistant-AGPL-3.0.txt")
    run(
        sys.executable,
        "-m",
        "PyInstaller",
        "--noconfirm",
        "--clean",
        "--windowed",
        "--onedir",
        "--noupx",
        "--name",
        "MailboxAssistant",
        "--additional-hooks-dir",
        "hooks",
        "--exclude-module",
        "tkinter",
        "--exclude-module",
        "PySide6.QtQml",
        "--exclude-module",
        "PySide6.QtQuick",
        "--exclude-module",
        "PySide6.QtWebEngineCore",
        "launcher.py",
    )
    bundle = ROOT / "dist" / "MailboxAssistant"
    # Python 3.12 ships an older VC runtime; Qt 6.11 needs the newer, backward-compatible one.
    qt = Path(PySide6.__file__).parent
    for name in (
        "vcruntime140.dll",
        "vcruntime140_1.dll",
        "msvcp140.dll",
        "msvcp140_1.dll",
        "msvcp140_2.dll",
        "msvcp140_codecvt_ids.dll",
    ):
        source = qt / name
        if not source.is_file():
            raise SystemExit("Qt redistributable runtime missing")
        shutil.copy2(source, bundle / "_internal" / name)
    shutil.copytree(license_dir, bundle / "LICENSES", dirs_exist_ok=True)
    shutil.copy2(ROOT / "README.md", bundle / "README.md")
    notice = (
        f"# Source and licenses ({VERSION})\n\n"
        "This app is AGPL-3.0; Python is PSF licensed. Qt/PySide/shiboken use LGPL-3.0 "
        "with their applicable third-party notices. "
        "PyInstaller includes its bootloader exception.\n\n"
        f"Application source: https://github.com/01121531/subandnew-api/tree/{VERSION}/desktop/mailbox-assistant\n\n"
        "The companion sources ZIP contains this application's source, "
        "build instructions and unmodified "
        "Qt/PySide corresponding source archives. See LICENSES for all copied notices. "
        "Runtime DLLs under _internal are dynamically loaded and replaceable. Reverse engineering "
        "for debugging modifications to LGPL components is not prohibited.\n\n"
        "Source provenance and archive digests: source-manifest.json.\n"
    )
    (bundle / "SOURCE-NOTICE.md").write_text(notice, encoding="utf-8")
    (bundle / "source-manifest.json").write_text(
        json.dumps(provenance, indent=2) + "\n", encoding="utf-8"
    )
    app_source = sources / "application" / "desktop" / "mailbox-assistant"
    for source in ROOT.rglob("*"):
        relative = source.relative_to(ROOT)
        if any(
            part
            in {
                "build",
                "dist",
                ".venv",
                "__pycache__",
                ".pytest_cache",
                ".mypy_cache",
                ".ruff_cache",
            }
            or part.endswith(".egg-info")
            for part in relative.parts
        ):
            continue
        if source.is_file():
            if source.suffix == ".spec":
                continue
            target = app_source / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, target)
    for name in ("LICENSE", "VERSION"):
        shutil.copy2(REPO / name, sources / "application" / name)
    shutil.copy2(bundle / "source-manifest.json", sources / "source-manifest.json")
    for source, suffix in ((bundle, "windows-amd64"), (sources, "sources")):
        target = ROOT / "dist" / f"mailbox-assistant-{VERSION}-{suffix}.zip"
        allowed_roots = None
        if suffix == "sources":
            allowed_roots = {"application", "source-manifest.json"} | {
                f"{name}-6.11.2.tar.gz" for name in SOURCES
            }
        archive(source, target, allowed_roots)
        with target.open("rb") as stream:
            checksum = hashlib.file_digest(stream, "sha256").hexdigest()
        target.with_suffix(".zip.sha256").write_text(
            f"{checksum}  {target.name}\n", encoding="ascii"
        )
        print(f"Built {target.name} ({target.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
