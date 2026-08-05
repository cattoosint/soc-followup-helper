#!/usr/bin/env python3
"""Build the zip to email in - just the tool, none of the local state.

    python make_package.py              mail-safe (default)
    python make_package.py --with-bat   keep .bat files as-is

Mail providers (Gmail included) block .bat attachments even inside a zip,
so by default the two .bat files ship as .bat.txt with a note explaining
the one-step rename. Everything else is .py/.txt/.md/.xlsx, which passes.

Leaves out the signed-in browser profile, run outputs, logs, screenshots
and caches, so nothing personal or mailbox-related travels with it.
"""

import sys
import zipfile
from datetime import datetime
from pathlib import Path

HERE = Path(__file__).resolve().parent

RENAME_NOTE = """READ ME FIRST
=============

Two files in this folder arrived with an extra ".txt" on the end:

    install.bat.txt
    run_followup.bat.txt

That is only because mail systems block .bat attachments. Rename both to
remove the ".txt" (right-click > Rename, delete the last four characters)
and they work normally:

    install.bat.txt        ->  install.bat
    run_followup.bat.txt   ->  run_followup.bat

Then double-click install.bat, and after that run_followup.bat.

If you would rather not rename anything, open a Command Prompt in this
folder and run these two lines instead - that is all the .bat files do:

    python -m pip install -r requirements.txt
    python soc_gui.py

Full instructions are in INSTALL.txt
"""

INCLUDE = [
    "soc_followup.py",      # the tool
    "soc_gui.py",
    "run_followup.bat",
    "install.bat",
    "requirements.txt",
    "INSTALL.txt",
    "README.md",
    "audit_simulation.py",  # so reviewers can run the checks themselves
    "test_edge_cases.py",
    "live_probe.py",
    "sample_cases.csv",        # sample inputs, so it can be tried immediately
    "test_cases_sample.xlsx",  # bare case numbers, like a real XSOAR export
    "test_filtered.xlsx",      # demonstrates Excel-filtered rows being skipped
]

# never package: signed-in session, run outputs, logs, screenshots, caches
EXCLUDE_PREFIXES = ("uc_profile", "owa_profile", "screenshots", "__pycache__",
                    "logs", "diagnostics_", "followup_results_",
                    "followup_review_", "followup_flagged_")


def main():
    mail_safe = "--with-bat" not in sys.argv
    stamp = datetime.now().strftime("%Y%m%d")
    out = HERE.parent / f"SOC_Followup_{stamp}.zip"

    missing, added, renamed = [], [], []
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
        for name in INCLUDE:
            path = HERE / name
            if not path.exists():
                missing.append(name)
                continue
            arc = name
            if mail_safe and name.lower().endswith(".bat"):
                arc = name + ".txt"      # .bat attachments get blocked
                renamed.append(name)
            z.write(path, arcname=f"SOC Followup/{arc}")
            added.append((arc, path.stat().st_size))
        if renamed:
            z.writestr("SOC Followup/READ ME FIRST.txt", RENAME_NOTE)
            added.append(("READ ME FIRST.txt", len(RENAME_NOTE)))

    print(f"Package: {out}")
    print(f"Size   : {out.stat().st_size / 1024:.0f} KB\n")
    print("Contents:")
    for name, size in added:
        print(f"   {name:<24} {size / 1024:7.1f} KB")
    if missing:
        print("\nNot found (skipped):")
        for name in missing:
            print(f"   {name}")

    leftovers = [p.name for p in HERE.iterdir()
                 if p.name.startswith(EXCLUDE_PREFIXES)]
    if leftovers:
        print(f"\nDeliberately left out ({len(leftovers)} item(s)): "
              f"{', '.join(sorted(leftovers)[:6])}"
              + (" ..." if len(leftovers) > 6 else ""))

    if renamed:
        print("\nMail-safe: " + ", ".join(renamed) + " packed as .bat.txt "
              "(mail systems block .bat).\n'READ ME FIRST.txt' explains the "
              "rename; INSTALL.txt has the manual commands.")
    else:
        print("\nWARNING: .bat files included as-is - Gmail and most mail "
              "gateways will block this zip.")

    exts = sorted({Path(n).suffix.lower() for n, _ in added})
    print(f"\nFile types in the zip: {', '.join(exts)}")


if __name__ == "__main__":
    main()
