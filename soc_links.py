"""SOC follow-up - no-install edition.

Uses ONLY the Python standard library: nothing to pip install, nothing to
get approved. It opens each case's Outlook search in your normal browser -
the one you are already signed into - and you click Reply all and Send.

    python soc_links.py cases.csv
    python soc_links.py cases.csv --html      (make a clickable list instead)
    python soc_links.py cases.csv --url https://outlook.cloud.microsoft/mail/

Save the XSOAR export as CSV. Case numbers are read from the first column,
or use --col 3 for the third column.
"""

import csv
import re
import sys
import time
import urllib.parse
import webbrowser
from pathlib import Path

URL = "https://outlook.office.com/mail/"
NUM = re.compile(r"\d{5,8}")


def read_cases(path, col=1):
    """Case numbers from a CSV column, or any 5-8 digit numbers in a text
    file. Order kept, duplicates dropped."""
    text = Path(path).read_text(encoding="utf-8-sig", errors="replace")
    out = []
    if str(path).lower().endswith(".csv"):
        rows = list(csv.reader(text.splitlines()))[1:]      # skip the header
        values = [r[col - 1] for r in rows if len(r) >= col]
    else:
        values = text.splitlines()
    for value in values:
        m = NUM.search(value)
        if m and m.group() not in out:
            out.append(m.group())
    return out


def search_url(base, num):
    """Outlook opens search results directly from a URL like
    .../mail/0/search?q=SOC123456"""
    base = base.rstrip("/")
    if base.endswith("/mail"):
        base += "/0"
    query = urllib.parse.quote(f"SOC{num}")
    return f"{base}/search?q={query}"


def write_html(cases, base, out_path):
    """A page of clickable searches - useful if you would rather not sit in
    a terminal, or want to hand the list to someone else."""
    rows = "\n".join(
        f'<tr><td>{i}</td><td>SOC{n}</td>'
        f'<td><a href="{search_url(base, n)}" target="_blank">open in Outlook</a>'
        f'</td><td><input type="checkbox"></td></tr>'
        for i, n in enumerate(cases, 1))
    out_path.write_text(f"""<!doctype html>
<meta charset="utf-8"><title>SOC follow-up ({len(cases)} cases)</title>
<style>
 body{{font:14px system-ui;margin:24px}} table{{border-collapse:collapse}}
 td,th{{border:1px solid #ccc;padding:6px 10px}} tr:nth-child(even){{background:#f6f6f6}}
</style>
<h2>SOC follow-up - {len(cases)} cases</h2>
<p>Click each link: Outlook opens with the case searched. Reply all, send,
then tick it off.</p>
<table><tr><th>#</th><th>Case</th><th>Search</th><th>Done</th></tr>
{rows}
</table>""", encoding="utf-8")


def main():
    argv = sys.argv[1:]
    files = [a for a in argv if not a.startswith("-")]
    if not files:
        sys.exit("usage: python soc_links.py cases.csv [--html] [--col N] "
                 "[--url <outlook url>]")
    base = argv[argv.index("--url") + 1] if "--url" in argv else URL
    col = int(argv[argv.index("--col") + 1]) if "--col" in argv else 1

    cases = read_cases(files[0], col)
    if not cases:
        sys.exit("No case numbers found - check the column with --col N")
    print(f"{len(cases)} case(s) to follow up\n")

    if "--html" in argv:
        out = Path(f"followup_links_{time.strftime('%Y%m%d_%H%M%S')}.html")
        write_html(cases, base, out)
        print(f"Wrote {out}")
        webbrowser.open(out.resolve().as_uri())
        return

    results = []
    for i, num in enumerate(cases, 1):
        print(f"[{i}/{len(cases)}] SOC{num}")
        webbrowser.open(search_url(base, num))
        answer = input("    [Enter]=replied | n=no mail found | s=skip | "
                       "q=stop > ").strip().lower()
        if answer == "q":
            break
        results.append((num, {"n": "NOT_FOUND", "s": "SKIPPED"}.get(answer, "SENT")))

    out = Path(f"followup_results_{time.strftime('%Y%m%d_%H%M%S')}.csv")
    with open(out, "w", newline="", encoding="utf-8-sig") as f:
        csv.writer(f).writerows([("case", "status")] +
                                [(f"SOC{n}", s) for n, s in results])
    todo = [n for n, s in results if s == "NOT_FOUND"]
    print(f"\nDone. {len(results)} processed, {len(todo)} with no mail found. "
          f"Log: {out.name}")
    for n in todo:
        print(f"   SOC{n}")


if __name__ == "__main__":
    main()
