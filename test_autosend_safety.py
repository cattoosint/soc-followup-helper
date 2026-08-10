#!/usr/bin/env python3
"""Auto-send safety tests.

Each case here comes from a defect found by review of the live behaviour.
Auto-send puts email in front of real recipients with no human check, so
every one of these must fail closed - when anything is ambiguous the case
goes back to the analyst.

    python test_autosend_safety.py
"""

import sys
from datetime import datetime
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import soc_followup as core

PASS, FAIL = [], []


def check(label, ok, detail=""):
    (PASS if ok else FAIL).append(label)
    print(f"[{'OK ' if ok else 'FAIL'}] {label}")
    if detail and not ok:
        print(f"        {detail}")


ALERTS = "alerts@example.com"
PHRASE = "follow up"
BODY = "SOC700001 alert\nPlease follow up on this case."

print("=" * 72)
print("AUTO-SEND SAFETY")
print("=" * 72)

# --- the sender must come from the real sender line ----------------------
print("\n-- sender identification --")

quoted = ("Jane Colleague Thu 8/7/2026 10:15 AM FYI - "
          "From: SOC Alerts <alerts@example.com> Sent: Wed 8/6/2026 "
          "To: Jane Subject: SOC700001")
sent, _ = core.should_auto_send(core.sender_part(quoted), BODY, PHRASE, ALERTS)
check("a quoted 'From:' inside a forwarded mail does not pass as the sender",
      not sent, f"sender_part -> {core.sender_part(quoted)!r}")

lower_to = "Jane Colleague to: alerts@example.com Thu 8/7/2026 10:15 AM"
sent, _ = core.should_auto_send(core.sender_part(lower_to), BODY, PHRASE, ALERTS)
check("a lowercase 'to:' recipient does not pass as the sender", not sent,
      f"sender_part -> {core.sender_part(lower_to)!r}")

starts_quoted = "From: SOC Alerts <alerts@example.com> Sent: Wed 8/6/2026"
check("text that is only a quoted header yields no sender",
      core.sender_part(starts_quoted) == "",
      f"got {core.sender_part(starts_quoted)!r}")

real = "AS ISS SOC ALERTS<alerts@example.com>   To:Analyst Wed 8/5/2026 1:10 PM"
sent, why = core.should_auto_send(core.sender_part(real), BODY, PHRASE, ALERTS)
check("a genuine sender line still matches", sent, why)

preview = ("AS ISS SOC ALERTS<alerts@example.com> Wed 8/5/2026 1:10 PM "
           "Please follow up - regards, Jane Colleague")
check("body text after the timestamp is not treated as the sender",
      "jane" not in core.sender_part(preview).lower(),
      f"sender_part -> {core.sender_part(preview)!r}")

# --- a too-short sender would match almost anything ----------------------
print("\n-- configuration sanity --")
sent, why = core.should_auto_send("AS ISS SOC ALERTS<alerts@example.com>",
                                  BODY, PHRASE, "SOC")
check("a very short configured sender is refused", not sent, why)

# --- timestamps decide which message is newest ---------------------------
print("\n-- timestamp reading --")
NOW = datetime(2026, 8, 9, 20, 0)          # Sunday evening

check("'Monitoring' is not read as Monday",
      core.parse_row_time("Monitoring alert 9:15 AM", NOW).day == NOW.day,
      str(core.parse_row_time("Monitoring alert 9:15 AM", NOW)))

check("'Satellite' is not read as Saturday",
      core.parse_row_time("Satellite feed 9:15 AM", NOW).day == NOW.day,
      str(core.parse_row_time("Satellite feed 9:15 AM", NOW)))

ts = core.parse_row_time("Sun 9:15 AM", NOW)
check("a mail labelled with today's weekday means a week ago",
      ts is not None and (NOW - ts).days == 7, str(ts))

ts = core.parse_row_time("Yesterday 11:30 PM", NOW)
check("'Yesterday' is yesterday", ts is not None and (NOW.day - ts.day) == 1,
      str(ts))

ts = core.parse_row_time("14:35", NOW)
check("a 24-hour clock is understood",
      ts is not None and (ts.hour, ts.minute) == (14, 35), str(ts))

ts = core.parse_row_time("11:30 PM", NOW)
check("a time later today is read as yesterday, not the future",
      ts is not None and ts <= NOW, str(ts))

ts = core.parse_row_time("13/8/2026", NOW)
check("a day above 12 forces day/month order regardless of locale",
      ts is not None and (ts.day, ts.month) == (13, 8), str(ts))

ts = core.parse_row_time("8/13/2026", NOW)
check("a month above 12 forces month/day order",
      ts is not None and (ts.day, ts.month) == (13, 8), str(ts))

check("the machine's own date order is used for ambiguous dates",
      isinstance(core.date_is_day_first(), bool))

# --- message headers must be recognised whatever the clock ---------------
print("\n-- 24-hour mailboxes --")
check("a 24-hour timestamp still marks a line as a message header",
      core.MSG_TIME_RE.search("AS SOC ALERTS<alerts@example.com> 14:35")
      is not None)
check("a 12-hour timestamp still does",
      core.MSG_TIME_RE.search("AS SOC ALERTS<alerts@example.com> 2:35 PM")
      is not None)

# --- the draft that gets sent must be the one that was judged ------------
print("\n-- the send is tied to the case --")


class Draft:
    def __init__(self, subject):
        self.subject = subject
        self.sent = False

    def is_displayed(self): return True
    def is_enabled(self): return True
    def click(self): self.sent = True
    @property
    def text(self): return ""
    def get_attribute(self, name):
        return self.subject if name in ("value", "title") else ""


class ComposePage:
    def __init__(self, subject):
        self.subject_el = Draft(subject)
        self.send_el = Draft("Send")

    def find_elements(self, by, sel):
        if "Subject" in sel or "subject" in sel:
            return [self.subject_el]
        if "Send" in sel:
            return [self.send_el]
        return []

    def find_element(self, by, sel):
        found = self.find_elements(by, sel)
        if found:
            return found[0]
        raise Exception("not found")


wrong = ComposePage("RE: SOC700099 a different case")
check("refuses to send a draft belonging to another case",
      core.click_send(wrong, num="700001") is False and not wrong.send_el.sent)

right = ComposePage("RE: SOC700001 Suspicious login")
check("sends when the draft is for this case",
      core.click_send(right, num="700001") is True and right.send_el.sent)

print("\n" + "-" * 72)
print(f"{len(PASS)} passed, {len(FAIL)} failed")
if FAIL:
    for f in FAIL:
        print("   FAILED:", f)
print("=" * 72)
sys.exit(1 if FAIL else 0)
