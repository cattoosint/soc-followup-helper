# CLAUDE.md

Read this before exploring the code — it should save you most of the
reading. Written for a SOC analyst's night-shift follow-up helper that
drives Outlook on the web.

## What it does

Reads a filtered XSOAR export (`.xlsx`/`.csv`), and for each case number
searches Outlook on the web, opens the newest matching mail, clicks
**Reply all**, and waits for the analyst to send. Optionally it can send by
itself under a strict rule. Outputs a results CSV, a colour-coded copy of
the analyst's sheet, and a detailed log.

## Layout

| File | Role |
|---|---|
| `soc_followup.py` | engine + CLI. Everything except the window. |
| `soc_gui.py` | tkinter window; runs the engine on a worker thread |
| `libs/` | bundled selenium/openpyxl — the target network has no PyPI |
| `drivers/` | bundled chromedriver 150/151 — driver downloads are blocked |
| `audit_simulation.py` | end-to-end run against a fake Outlook DOM |
| `test_edge_cases.py`, `test_autosend_safety.py`, `test_data_integrity.py` | offline suites |
| `live_probe.py` | one case against a real mailbox; sends nothing |
| `send_test_mail.py` | creates test mail (test mailboxes only) |

Entry points: `python soc_gui.py`, `START HERE.bat`, or
`python soc_followup.py --csv <sheet> [--check|--driver-info|--diagnostics]`.

## How the two halves talk

The engine never imports tkinter. It calls four methods on a `ui` object:

- `ui.log(msg)`
- `ui.case_update(num, status, detail)` — drives the tracker table
- `ui.ask(prompt, choices, kind=...)` — blocks the worker thread
- `ui.ask_or_watch(...)` — same, but returns `"auto"` when a watcher fires
- `ui.stop_requested()` — optional; checked between cases

`ConsoleUI` (engine) and `GuiUI` (window) implement it. Keep it that way —
the tests supply their own scripted UI.

## Invariants — these exist because they broke in testing

**Auto-send is the dangerous part.** It sends mail with no human check. It
must fail closed: when anything is ambiguous, hand the case back.

- decide from the **newest message by timestamp**, never by screen
  position. "Show newest messages on top" is a per-mailbox setting and
  inverts the order — that made it read the original alert, which always
  matches the rule.
- take the sender from the **real sender line only**. Element text can
  carry body content and quoted headers; a quoted `From:` used to pass as
  the sender. `sender_part()` drops anything at/after a recipient or
  quoted-header marker and after the first timestamp.
- the reading pane must be showing **that case number** before anything is
  read or clicked — the previous case's mail lingers on screen.
- never match a case number as a substring: `label_matches_case()` uses
  digit boundaries so `610529` does not match `SOC1610529`.
- comparisons go through `normalize_text()`: Outlook wraps long addresses
  mid-word (`soc alerts@x.com`) and mixes private-use icon glyphs in.

**Reporting must not overstate.** A green row means the analyst can stop
thinking about that case. Unverified sends are amber "Replied
(unconfirmed)", duplicate rows are grey, and results finished before a
crash are still written.

## Running the tests — no mailbox or network needed

```
python audit_simulation.py      # simulated Outlook, end to end
python test_edge_cases.py
python test_autosend_safety.py
python test_data_integrity.py
```

All print PASS/FAIL per check and exit non-zero on failure. Run them after
any change to matching, auto-send or the export. `live_probe.py <case>`
checks the selectors against a real mailbox and sends nothing.

## Environment facts

- Target machines have **no PyPI access, no driver downloads, and GitHub
  may be blocked**. Hence `libs/` and `drivers/` in the repo. Do not add a
  dependency without checking it is pure Python and bundling it.
- `psutil` is optional; the code degrades if missing.
- Chrome must be present. `--driver-info` reports the version and which
  bundled driver will be used.
- The signed-in session lives in `uc_profile/` — treat it as credential
  material. It is gitignored, as is `soc_settings.json` (holds the
  site-specific auto-send sender; never commit a real address).
- `_release_stale_profile()` kills only Chrome processes whose command line
  names this profile. Do not loosen that.

## Gotchas that cost time

- **Never rewrite these files with PowerShell `Get-Content | Set-Content`.**
  PS 5.1 reads UTF-8 as ANSI and mangles non-ASCII; it silently corrupted a
  control character in the test harness and made every audit case fail.
  Use escapes (``), not literal glyphs.
- Piping a long-running Python script through `Select-Object -First N`
  makes it hang on a closed pipe. Redirect to a file instead.
- OWA's search suggestions use the same `role=listbox/option` markup as the
  message list. Result lookups must stay scoped to the message list
  (`MESSAGE_LIST_SELECTORS`), and generic-fallback rows must carry a
  timestamp.
- `subject:` search operators match nothing in practice. Plain text works:
  `SOC123456` then `SOC 123456` (the spaced form also finds `[SOC#123456]`
  and `SOC-123456`, because Outlook splits on punctuation).
- Chrome cannot automate a profile another Chrome instance holds, and
  Chrome 136+ refuses automation on the default profile directory.

## Working style that fits this project

Changes here can cause an analyst to reply to the wrong case or believe a
case was handled when it was not. Prefer a guard that fails closed, add a
test that reproduces the failure first, and say plainly in the log why a
case was skipped. The offline suites are quick — run them.
