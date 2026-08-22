#!/usr/bin/env python3
"""Regenerate internal/core/testdata/python_truth.json from the Python original.

That fixture is the arbiter for the most dangerous logic in this tool - case
matching, sender parsing, time ordering and auto-send. CLAUDE.md tells you to
fix the code rather than the fixture when they disagree, and to regenerate it
only if the Python original itself changed.

Until now that instruction could not be carried out: the generator did not
exist in this repository, and no document said where the Python original lived.
A documented step nobody can perform is the same class of defect as a test that
cannot fail.

It works by keeping every INPUT in the existing fixture and recomputing the
answers with the Python implementation. So it cannot quietly invent new cases,
and it cannot be used to make a failing Go test pass by changing the expected
values - regenerating without a real change to the Python produces a file that
is byte-identical.

Usage:

    python tools/gen_truth.py <path to soc_followup.py>
    python tools/gen_truth.py <path to soc_followup.py> --check

--check recomputes and reports differences WITHOUT writing. Run that first:
if it reports anything, the Go code and the Python have genuinely diverged and
you need to decide which is right before touching the fixture.
"""

import argparse
import datetime
import importlib.util
import json
import pathlib
import sys

HERE = pathlib.Path(__file__).resolve().parent
FIXTURE = HERE.parent / "internal" / "core" / "testdata" / "python_truth.json"


def load_original(path):
    """Import soc_followup.py as a module without needing it on sys.path."""
    spec = importlib.util.spec_from_file_location("soc_followup_original", path)
    if spec is None or spec.loader is None:
        raise SystemExit("could not load %s as a Python module" % path)
    module = importlib.util.module_from_spec(spec)
    try:
        spec.loader.exec_module(module)
    except Exception as exc:  # noqa: BLE001 - report, do not hide
        raise SystemExit(
            "importing %s failed: %s\n"
            "The original imports selenium at module level in some versions; "
            "run this where those imports resolve, or stub them." % (path, exc)
        )
    for name in ("build_queries", "label_matches_case", "normalize_text",
                 "sender_part", "should_auto_send", "parse_row_time"):
        if not hasattr(module, name):
            raise SystemExit(
                "%s has no %s() - this does not look like the original "
                "implementation the fixture came from." % (path, name))
    return module


def iso(stamp):
    if stamp is None:
        return ""
    return stamp.strftime("%Y-%m-%dT%H:%M:%S")


def regenerate(orig, old):
    """Recompute every answer, keeping every input exactly as it is."""
    new = {}

    # The reference "now" the time cases are resolved against. Kept verbatim:
    # changing it would silently re-date every expectation in the fixture.
    new["now"] = old["now"]

    new["normalize_text"] = [
        {"in": c["in"], "out": orig.normalize_text(c["in"])}
        for c in old["normalize_text"]
    ]

    new["sender_part"] = [
        {"header": c["header"], "sender": orig.sender_part(c["header"])}
        for c in old["sender_part"]
    ]

    new["label_matches_case"] = [
        {"label": c["label"], "num": c["num"],
         "match": bool(orig.label_matches_case(c["label"], c["num"]))}
        for c in old["label_matches_case"]
    ]

    new["build_queries"] = {
        num: list(orig.build_queries(num)) for num in old["build_queries"]
    }

    new["should_auto_send"] = []
    for c in old["should_auto_send"]:
        send, why = orig.should_auto_send(
            c["header"], c["body"], c["keyword"], c["sender"])
        new["should_auto_send"].append({
            "header": c["header"], "body": c["body"],
            "keyword": c["keyword"], "sender": c["sender"],
            "send": bool(send), "why": why,
        })

    # Both date orderings, because Outlook follows the Windows short-date
    # setting and "10/8/2026" means different months in each.
    for key, day_first in (("parse_row_time_day_first", True),
                           ("parse_row_time_month_first", False)):
        rows = []
        now = datetime.datetime.fromisoformat(old["now"]) if old.get("now") else None
        for c in old[key]:
            stamp = call_parse_row_time(orig, c["label"], now, day_first)
            rows.append({"label": c["label"], "stamp": iso(stamp)})
        new[key] = rows

    return new


def call_parse_row_time(orig, label, now, day_first):
    """Call parse_row_time with the date order pinned, however it exposes it."""
    # The original caches the Windows short-date setting in a module global.
    # Pinning it is not optional: without it the fixture would be regenerated
    # against whatever locale this machine happens to have, and the two
    # orderings would silently become identical.
    if hasattr(orig, "set_day_first_for_test"):
        orig.set_day_first_for_test(day_first)
    elif hasattr(orig, "_DAY_FIRST"):
        orig._DAY_FIRST = day_first
    elif hasattr(orig, "DATE_IS_DAY_FIRST"):
        orig.DATE_IS_DAY_FIRST = day_first
    else:
        raise SystemExit(
            "cannot pin the date order in the original: it exposes none of "
            "set_day_first_for_test(), _DAY_FIRST or DATE_IS_DAY_FIRST. "
            "Regenerating would silently use this machine's locale.")
    try:
        return orig.parse_row_time(label, now)
    except Exception:  # noqa: BLE001 - an unparseable label is a real answer
        return None


def differences(old, new):
    out = []
    for key in sorted(set(old) | set(new)):
        if key not in old:
            out.append("  + section %s is new" % key)
            continue
        if key not in new:
            out.append("  - section %s disappeared" % key)
            continue
        if old[key] == new[key]:
            continue
        if isinstance(old[key], list):
            for i, (a, b) in enumerate(zip(old[key], new[key])):
                if a != b:
                    out.append("  ~ %s[%d]\n      was %s\n      now %s"
                               % (key, i, json.dumps(a, ensure_ascii=False),
                                  json.dumps(b, ensure_ascii=False)))
        else:
            out.append("  ~ %s\n      was %s\n      now %s"
                       % (key, json.dumps(old[key], ensure_ascii=False),
                          json.dumps(new[key], ensure_ascii=False)))
    return out


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("original", help="path to the Python soc_followup.py")
    ap.add_argument("--check", action="store_true",
                    help="report differences without writing the fixture")
    args = ap.parse_args()

    if not FIXTURE.exists():
        raise SystemExit("fixture not found at %s" % FIXTURE)
    old = json.loads(FIXTURE.read_text(encoding="utf-8"))

    orig = load_original(args.original)
    new = regenerate(orig, old)

    diffs = differences(old, new)
    if not diffs:
        print("No change: the Python original still produces this fixture "
              "exactly.\n(%d sections checked.)" % len(new))
        return 0

    print("The Python original no longer agrees with the fixture:\n")
    print("\n".join(diffs))
    print()
    if args.check:
        print("--check: nothing written. Decide which side is right BEFORE "
              "regenerating.\nIf the Go code is what changed, the change is "
              "wrong - fix the code, not the fixture.")
        return 1

    FIXTURE.write_text(
        json.dumps(new, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
    print("Written: %s" % FIXTURE)
    print("Say plainly in the commit message WHAT changed in the Python and "
          "why regenerating was right.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
