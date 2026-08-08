from pathlib import Path
import re

import yaml


ROOT = Path(__file__).resolve().parents[1]
KEYS_FILE = ROOT / "i18n" / "keys.go"
LOCALES_DIR = ROOT / "i18n" / "locales"


def main() -> None:
    source = KEYS_FILE.read_text(encoding="utf-8")
    keys = re.findall(r'Msg\w+\s*=\s*"([^"]+)"', source)
    for locale_path in sorted(LOCALES_DIR.glob("*.yaml")):
        existing = yaml.safe_load(locale_path.read_text(encoding="utf-8")) or {}
        filtered = {key: existing.get(key, key) for key in keys}
        locale_path.write_text(
            yaml.safe_dump(filtered, allow_unicode=True, sort_keys=False),
            encoding="utf-8",
        )
        print(f"{locale_path.name}: {len(existing)} -> {len(filtered)}")


if __name__ == "__main__":
    main()
