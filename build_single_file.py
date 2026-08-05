#!/usr/bin/env python3
"""Merge the tool into ONE .py file, for when zips can't be shared.

    python build_single_file.py

Produces  SOC_Followup_single.py  (and a .txt twin for emailing, since some
gateways allow .txt but not .py). Rename the .txt back to .py to use it.
"""

import re
from pathlib import Path

HERE = Path(__file__).resolve().parent
OUT_PY = HERE.parent / "SOC_Followup_single.py"
OUT_TXT = HERE.parent / "SOC_Followup_single.py.txt"

HEADER = '''#!/usr/bin/env python3
"""SOC Nightshift Follow-up - single-file build.

Everything in one file so it can be shared where zips are blocked.

    python SOC_Followup_single.py                 open the window
    python SOC_Followup_single.py --csv x.xlsx --check     check an export

Needs:  pip install selenium openpyxl psutil
"""
'''

DISPATCH = '''

# ------------------------------------------------------------- entry point

if __name__ == "__main__":
    import sys as _sys
    if len(_sys.argv) > 1 and _sys.argv[1].startswith("-"):
        cli_main()          # command line: --csv, --check, --diagnostics ...
    else:
        gui_main()          # no arguments: open the window
'''


def strip_main_block(src):
    """Remove the trailing  if __name__ == "__main__":  block."""
    return re.split(r'\nif __name__ == ["\']__main__["\']:', src)[0].rstrip() + "\n"


def main():
    core = strip_main_block((HERE / "soc_followup.py").read_text(encoding="utf-8"))
    gui = strip_main_block((HERE / "soc_gui.py").read_text(encoding="utf-8"))

    # both files define main(); keep them apart
    core = re.sub(r"^def main\(\):", "def cli_main():", core, flags=re.M)
    gui = re.sub(r"^def main\(\):", "def gui_main():", gui, flags=re.M)

    # the GUI half talks to the core half through `core.` - in one file that
    # is simply this module
    gui = gui.replace("import soc_followup as core\n", "")

    # drop each half's module docstring; the merged file has its own
    core = re.sub(r'^#!.*\n', "", core)
    core = re.sub(r'^""".*?"""\n', "", core, count=1, flags=re.S)
    gui = re.sub(r'^#!.*\n', "", gui)
    gui = re.sub(r'^""".*?"""\n', "", gui, count=1, flags=re.S)

    merged = (HEADER
              + "\n# ============ engine (was soc_followup.py) ============\n"
              + core
              + "\n\nimport sys as _sys_self\n"
                "core = _sys_self.modules[__name__]  # the GUI half calls core.*\n"
              + "\n# ============ window (was soc_gui.py) ============\n"
              + gui
              + DISPATCH)

    OUT_PY.write_text(merged, encoding="utf-8")
    OUT_TXT.write_text(merged, encoding="utf-8")
    lines = merged.count("\n")
    print(f"Wrote {OUT_PY}")
    print(f"      {OUT_TXT}   (email this one, rename back to .py)")
    print(f"{lines} lines, {len(merged) / 1024:.0f} KB")


if __name__ == "__main__":
    main()
