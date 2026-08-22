# socfollowup (Go, zero dependencies)

A port of the SOC night-shift follow-up helper from Python/Selenium to
Go. It ships as **one executable**: no Python, no `chromedriver`,
no `pip install`. Chrome is the only thing that has to already be on the
machine.

It has been run end to end against a real mailbox on both Chrome and Edge -
search, Reply all, the analyst's send, auto-send, Sent Items verification - and
it builds and runs on a locked-down corporate desktop. What it has **not** done
is a full shift on a corporate Outlook tenant; §"What has actually been proven"
is precise about the difference and why it matters.

## Why this exists

The Python build needs `libs/` (selenium, openpyxl and friends) and a 40 MB
**unsigned** `chromedriver.exe`. Getting that bundle onto a locked-down desktop
was the whole problem. This talks the Chrome DevTools Protocol directly, so
the driver disappears, and Go links everything into a single binary, so the
dependency folder disappears too.

What that trades away, honestly:

| | Python build | This |
|---|---|---|
| Files to get in | `.py` sources + `libs/` + `chromedriver.exe` | 110 KB of source, or one `.exe` |
| chromedriver | required, 40 MB, unsigned | **not needed** |
| Reviewable by a human | yes — every line is readable source | no — a compiled binary |
| Proven against a real mailbox | yes, 50-case run | yes on a personal tenant (Chrome and Edge); corporate tenant **not yet** |
| Analyst UI | tkinter window with a live tracker | local page in your browser, or console-only with `-tags cli` |

If your reviewers want to *read* what they approve, the Python build is the
better story. This one is better when the blocker is getting bytes in at all.

## Build

Needs Go 1.24+ (a portable extract is fine — no admin, no installer):

```
go build -ldflags="-s -w" -trimpath -o bin/socfollowup.exe ./cmd/socfollowup
```

Cross-compile from anywhere:

```
GOOS=windows GOARCH=amd64 go build -o bin/socfollowup.exe ./cmd/socfollowup
```

### If files went missing in transit

A mail gateway that sanitises attachments strips any file holding an HTML
document, and two here are exactly that: `internal/web/page.go` (the tracker
page) and `internal/fakeowa/server.go` (the stand-in mailbox `--self-test`
drives). `main.go` imports both, so the build fails naming a missing *package*.

Build without them:

```
go build -tags cli -ldflags="-s -w" -trimpath -o bin/socfollowup.exe ./cmd/socfollowup
```

That costs you the tracker window and `--self-test`; `--diagnose` becomes your
only pre-flight, so do not skip it. Searching, Reply all, the send, auto-send
and its guards, the Sent Items check and both output files are unaffected. It
is a smaller tool, not a smuggled one - a `cli` build genuinely contains no
HTML. Prefer a full copy through a sanctioned route; `DEPLOYING.md` covers
both.

## Prove it works before trusting it

On a new machine, run the self-test. It drives a **stand-in mailbox served from
the binary itself** — nothing touches a real mailbox and no mail can be sent:

```
socfollowup.exe --self-test
```

It checks the four things that matter:

```
[ok  ] auto-sends when the newest mail really is from the sender - SOC610529
[ok  ] refuses when the sender is only quoted                     - SOC700001
[ok  ] flags a case with no matching mail                         - SOC999999
[ok  ] wrote a colour-coded review sheet
```

## Use

**Double-click the executable.** It serves a tracker page on loopback and opens
it in your normal browser (not the automated one, which is busy driving
Outlook). Pick the export with *Browse...* or just drag it onto the page, hit
*Check export first* to see what it would do, then *Start run*. You get live
tiles, a colour-coded case table, the log, and the Sent / Skip / Retry / Stop
buttons.

**Give it the `.xlsx`, not a CSV.** Rows you filtered out in Excel are skipped -
but a filter only survives inside a workbook. A CSV has no hidden rows, so
every row in it gets followed up, including the ones you filtered out. The page
says so if you point it at one.

The page listens on 127.0.0.1 only and every request must carry a token minted
at startup - it can cause mail to be sent, so another process on the machine
must not be able to drive it.

From a command line instead:

```
socfollowup.exe --csv "shift.xlsx" --check     # what would this run do?
socfollowup.exe --csv "shift.xlsx"             # work through it, in the console
socfollowup.exe --ui                           # the tracker page
```

Auto-send is off unless you ask for it, and refuses to start without a sender:

```
socfollowup.exe --csv "shift.xlsx" --auto-send --auto-sender "someone@example.com"
```

`--auto-sender` is site-specific and is **never committed**. Put it in
`soc_settings.json` beside the executable (gitignored) or pass it on the
command line.

For what the interface looks like and how to use it, see `USING.md`.

## Layout

| Path | Role |
|---|---|
| `internal/core` | matching, timestamps, auto-send rules, sheet read/write. No browser. |
| `internal/cdp` | DevTools client and WebSocket, written here (chromedp is gone) |
| `internal/xlsx` | .xlsx reading and writing (replaces excelize) |
| `internal/browser` | the page primitives and the Outlook logic |
| `internal/engine` | per-case orchestration, the run loop, result files |
| `internal/web` | the tracker page, served over loopback |
| `internal/fakeowa` | stand-in mailbox — backs the tests *and* `--self-test` |
| `cmd/socfollowup` | CLI and the console front-end |
| `cmd/chromecheck` | one-line answer to "can this machine's Chrome be driven?" |

## Tests

Everything runs offline. The browser tests drive real Chrome against the
stand-in mailbox, so the selectors and guards are exercised for real.

```
go test ./...
```

`internal/core/testdata/python_truth.json` is generated from the **Python
original** and asserted against on every run, so the matching, timestamp and
auto-send answers cannot drift from the version that was reviewed and proven
live.

## Invariants — these exist because they broke in testing

Carried over from the Python build; the reasons have not changed.

- Decide from the **newest message by timestamp**, never by screen position.
  "Show newest messages on top" is a per-mailbox setting and inverts the order.
- Take the sender from the **real sender line only**. A quoted `From:` inside a
  forwarded mail must never pass as the sender.
- The reading pane must be showing **that case number** before anything is read
  or clicked — the previous case's mail lingers on screen.
- Never match a case number as a substring: `610529` must not match
  `SOC1610529`.
- A green row in the review sheet means the analyst can stop thinking about
  that case. Unverified sends are amber, duplicates grey.

Two more that this port added, both found by its own tests:

- **Element refs are stamped once and reused.** Re-stamping on every query
  invalidated handles a caller was still holding, and a click then waited
  forever for an element that no longer existed.
- **The search box is cleared through the value *property*, not the
  attribute.** `chromedp.Clear` sets the attribute, so each query was appended
  to the last — `SOC610529SOC700001` matches the *old* case. The typed text is
  now read back and the search refused if it did not land.

## What has actually been proven, and what has not

Be precise about this. Overstating what has been verified is the failure mode
this whole tool is built to avoid, and that applies to its own claims.

**Proven, on a real mailbox** (a personal Outlook tenant, supervised,
2026-08-21): sign-in, search, opening the newest match, Reply all, the Sent
Items check, and the guard that refuses to call an older reply proof of
tonight's send. That run found three defects no offline test could have - see
`AUDIT.md`, "Found by the supervised live run".

**Proven offline**: everything else. The full suite drives a real headless
browser against a stand-in mailbox, and `internal/core/testdata/python_truth.json`
holds 97 answers from the Python original that the suite asserts against.

**Proven on a locked-down corporate desktop** (2026-08-22): the source
transfers, builds offline with the bundled Go toolchain, and the executable
runs there. That is the deployment story, not the mailbox story.

**Not proven**: a run against a real **corporate** tenant. That is a different
OWA build from the consumer one, and the three defects the live run found were
all build-specific - different markup, different timing. Run
`socfollowup.exe --diagnose SOC<case>` there before trusting a shift to it; it
sends nothing and reports which link in the chain breaks.

**Never proven, by design**: auto-send against a corporate tenant. Leave it
off until the above is done.

> The self-test names a review sheet in its last line. That file is written
> into a temporary directory and removed when the test finishes - it is proof
> the writer works, not an artifact to go and open. Real runs write beside the
> executable, as `USING.md` describes.

## Documents

| File | What it is |
|---|---|
| `DESIGN.md` | the specification: the intended run, every feature and why, and where each part lives |
| `USING.md` | how to run it, for the analyst |
| `DEPLOYING.md` | getting it onto a machine with no network |
| `CLAUDE.md` | orientation for whoever changes the code next |
