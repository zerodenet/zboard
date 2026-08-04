from pathlib import Path

path = Path("scripts/apply-protocol-effects.py")
source = path.read_text()
old = "r'^        \"200\":\\n.*?(?=^        \"[0-9]{3}\":|\\Z)'"
new = "r'^        \"20[01]\":\\n.*?(?=^        \"[0-9]{3}\":|\\Z)'"
if source.count(old) != 1:
    raise SystemExit(f"expected one OpenAPI success response pattern, found {source.count(old)}")
path.write_text(source.replace(old, new, 1))
