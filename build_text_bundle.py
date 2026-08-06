#!/usr/bin/env python3
"""Turn the package into plain text that survives any mail filter.

    python build_text_bundle.py

Writes two things to the Desktop:

  SOC_Followup_package.b64.txt   the whole zip as base64 text - paste it into
                                 a file at the other end and unpack it
  unpack.py.txt                  ten lines that rebuild the folder from it

Base64 is used instead of raw source because email and chat clients like to
re-wrap long lines, and Python cares about indentation. Base64 survives
that; source code does not.
"""

import base64
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
DESKTOP = HERE.parent

UNPACK = '''"""Rebuild the SOC Followup folder from the base64 text file.

Put this next to SOC_Followup_package.b64.txt, rename both to remove the
.txt on this one, then run:   python unpack.py
"""
import base64, io, zipfile, pathlib

src = pathlib.Path("SOC_Followup_package.b64.txt")
data = "".join(line.strip() for line in src.read_text().splitlines()
               if line.strip() and not line.startswith("#"))
zipfile.ZipFile(io.BytesIO(base64.b64decode(data))).extractall(".")
print("Unpacked. Open the 'SOC Followup' folder and read INSTALL.txt")
'''


def main():
    # make sure the zip is current
    subprocess.run([sys.executable, str(HERE / "make_package.py")],
                   check=True, capture_output=True)
    zips = sorted(DESKTOP.glob("SOC_Followup_*.zip"))
    if not zips:
        sys.exit("no package zip found - run make_package.py first")
    zip_path = zips[-1]

    raw = base64.b64encode(zip_path.read_bytes()).decode()
    lines = [raw[i:i + 76] for i in range(0, len(raw), 76)]

    header = [
        "# SOC Nightshift Follow-up - package as base64 text",
        "#",
        "# 1. Save this whole message as:  SOC_Followup_package.b64.txt",
        "# 2. Save the unpack script next to it as:  unpack.py",
        "# 3. Run:  python unpack.py",
        "#",
        "# Lines starting with # are ignored by the unpacker.",
        f"# source: {zip_path.name}, {zip_path.stat().st_size / 1024:.0f} KB",
        "#",
    ]

    out = DESKTOP / "SOC_Followup_package.b64.txt"
    out.write_text("\n".join(header + lines) + "\n", encoding="utf-8")
    (DESKTOP / "unpack.py.txt").write_text(UNPACK, encoding="utf-8")

    print(f"Wrote {out.name}  ({out.stat().st_size / 1024:.0f} KB, "
          f"{len(lines)} lines)")
    print(f"Wrote unpack.py.txt  ({len(UNPACK.splitlines())} lines)")
    print("\nBoth are plain text - no attachments, no zip, no executables.")


if __name__ == "__main__":
    main()
