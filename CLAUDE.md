# CLAUDE.md

## START HERE — do these five things, in this order

If you have just been handed this repository, do not start by reading source
files. Do this instead. About fifteen minutes, most of it waiting.

**1. Find the Go toolchain.** Every command below starts with `go`, and on a
locked-down machine it is usually not on PATH:

```
where go
dir C:\Users\%USERNAME%\go-toolchain\go\bin\go.exe
dir "C:\Program Files\Go\bin\go.exe"
dir %LOCALAPPDATA%\Programs\Go\bin\go.exe
```

A portable toolchain unzipped anywhere works — Go needs no installer. Put its
`bin` on PATH, set `GOTOOLCHAIN=local`, leave `GOFLAGS` unset. **There is no
network**: no module proxy, no downloads. Never run `go mod tidy` — it reaches
for modules `go build` never needs.

**2. Read two sections, and only two, before touching anything:**

- **`DESIGN.md` §2** — the run this tool is built around, step by step.
- **`DESIGN.md` §5** — auto-send, the dangerous part.

Those two decide whether any change you are about to make is acceptable.

**3. Build it and prove it works here:**

```
go build ./... && go vet ./...
go build -ldflags="-s -w" -trimpath -o bin/socfollowup.exe ./cmd/socfollowup
bin/socfollowup.exe --self-test
```

`--self-test` drives the whole tool against a stand-in mailbox in about fifteen
seconds. No mailbox, no network, no credentials, and it sends no mail anywhere.
If it passes, the tool works on this machine.

**If the build fails naming `internal/web` or `internal/fakeowa` as missing**,
this copy arrived incomplete - a mail gateway strips files holding an HTML
document, and `page.go` is the tracker page. Build console-only with
`go build -tags cli ...` and read "If files went missing in transit" in
`DEPLOYING.md`. You lose the tracker window and `--self-test`, nothing else.

**4. Start the tests now, and read while they run:**

```
go test ./... -count=1
```

**About twelve minutes**, mostly silent — `internal/engine` alone is ten of
them, because it runs a full 30-case shift. **It has not hung.** Let it finish.

**5. Only now go to the code**, via the question-to-file map in `DESIGN.md` §6.
It names the function to start at, not just the file. Do not read the tree.

### Before you say you are done

```
go build ./... && go vet ./... && go test ./... -count=1
bin/socfollowup.exe --self-test
```

If you changed matching, sender parsing, time ordering or auto-send, the
conformance test against the Python original is the arbiter — not your
reasoning. See "Regenerating the arbiter" at the end of this file.

### Which file answers which question

| You want to | Read |
|---|---|
| change behaviour, or know why something is as it is | `DESIGN.md` — the specification |
| know what has actually been proven, and what has not | `README.md`, "What has actually been proven" |
| know which mistakes this codebase attracts | `AUDIT.md` — 37 confirmed defects, each with what it would have cost the analyst |
| get it onto a locked-down machine, or run it against a real mailbox for the first time | `DEPLOYING.md` — including the authorisation to settle first |
| know what the analyst actually sees | `USING.md` |

---

**`DESIGN.md` is the specification** - what the tool is for, the run it is
built around, every feature and why it exists, and a file-by-file map. Read
that if you are about to change behaviour. Read this one first if you are
about to change code.

Read this before exploring the code — it should save you most of the reading.
Written for a SOC analyst's night-shift follow-up helper that drives Outlook on
the web.

## What it is

A Go port of a working Python/Selenium tool. Same job: read a filtered XSOAR
export, and for each case number search Outlook on the web, open the newest
matching mail, click **Reply all**, and wait for the analyst to send.
Optionally send by itself under a strict rule. Outputs a results CSV, a
colour-coded copy of the analyst's sheet, and a live tracker page.

**The Python original still exists and is the one with real-world mileage** (a
50-case run against a real mailbox). This port has never faced a corporate
tenant. If the two disagree about behaviour, the Python one is right — see
"Conformance" below, which is how that is enforced mechanically.

**Why Go:** the Python build needs an interpreter, a `libs/` folder, and a
40 MB unsigned `chromedriver.exe`. Getting that onto a locked-down desktop was
the whole problem. internal/cdp speaks the DevTools Protocol to Chrome directly, so
the driver disappears; Go links everything into one executable, so `libs/`
disappears too. The trade is that the shipped artefact is an unsigned binary
rather than readable source — which is *harder* to get past application
allowlisting, not easier. Building on the target machine from this source
sidesteps that.

## Build and test

**No dependencies.** Standard library only - no `vendor/`, no module proxy,
nothing to fetch. That is deliberate and load-bearing: see "Why there are no
dependencies" below before adding one.

```
go build -ldflags="-s -w" -trimpath -o bin/socfollowup.exe ./cmd/socfollowup
go test ./...
```

Works with the network off: `GOPROXY=off GOTOOLCHAIN=local go build ./...`

**Needs Go 1.24+.** `go.mod` says `go 1.24.0` because that is what the target
site has; newer toolchains build it fine. `DEPLOYING.md` covers the rest of the
locked-down machine story.

**Prove it works on a machine before trusting it with a mailbox:**

```
socfollowup.exe --self-test
```

That drives a stand-in mailbox served from inside the binary. Nothing leaves
the machine and no mail can be sent.

The analyst-facing guide - what the interface looks like, how a shift is
worked through, and how to change the page - is in `USING.md`.

## Layout

| Path | Role |
|---|---|
| `internal/core` | matching, timestamps, auto-send rules, sheet read/write. **No browser.** |
| `internal/cdp` | DevTools client and WebSocket - launch Chrome, evaluate JS, keys, clicks, screenshots |
| `internal/xlsx` | reads and writes the slice of .xlsx this tool needs |
| `internal/browser` | page primitives (`page.go`) and Outlook logic (`outlook.go`) |
| `internal/engine` | per-case orchestration, the run loop, result files |
| `internal/web` | the tracker page, served over loopback |
| `internal/fakeowa` | stand-in mailbox — backs the tests *and* `--self-test` |
| `cmd/socfollowup` | CLI, console front-end, self-test |
| `cmd/chromecheck` | one-line answer to "can this machine drive a browser at all?" |

## How the halves talk

The engine never imports a front-end. It calls five methods on a `UI`:

- `Log(msg)`
- `CaseUpdate(num, status, detail)` — drives the tracker table
- `Ask(prompt, choices, kind)` — blocks the run
- `AskOrWatch(prompt, choices, kind, watch)` — same, but returns `"auto"` when
  the watcher fires (the draft closing on its own — the analyst pressed Send in
  Outlook, which is what they actually do)
- `StopRequested()` — checked between cases

`consoleUI` (cmd), `web.Server`, and the tests each implement it. Keep it that
way.

## Conformance — do not skip this

`internal/core/testdata/python_truth.json` holds 97 answers generated from the
**Python original**: sender extraction, digit-boundary case matching, timestamp
parsing under both date orders, and every auto-send decision including the
exact reason strings. `conformance_test.go` asserts against it on every run.

If you change anything in `internal/core`, that fixture is the arbiter. A
failure means you have drifted from behaviour that was reviewed and proven
live — fix the code, not the fixture. Only regenerate it if the Python original
itself changed, and say so plainly.

## Invariants — these exist because they broke in testing

Carried from the Python build; the reasons have not changed.

**Auto-send is the dangerous part.** It sends mail with no human check. It must
fail closed: when anything is ambiguous, hand the case back.

- Decide from the **newest message by timestamp**, never by screen position.
  "Show newest messages on top" is a per-mailbox setting and inverts the order.
- Take the sender from the **real sender line only**. `SenderPart()` drops
  anything at or after a recipient or quoted-header marker, and after the first
  timestamp — a quoted `From:` used to pass as the sender.
- The reading pane must be showing **that case number** before anything is read
  or clicked; the previous case's mail lingers on screen.
- Never match a case number as a substring. `LabelMatchesCase()` checks digit
  boundaries by hand, because Go's regexp engine (RE2) has **no lookbehind** —
  the Python original used `(?<!\d)`. Do not "simplify" it to `strings.Contains`.
- Comparisons go through `NormalizeText()`: Outlook wraps long addresses
  mid-word and mixes private-use icon glyphs into the same text.

**Reporting must not overstate.** A green row means the analyst can stop
thinking about that case. Unverified sends are amber "Replied (unconfirmed)",
duplicates grey, and results finished before a crash are still written.

**Give it the `.xlsx`, not a CSV.** Rows filtered out in Excel are skipped, but
a filter only survives inside a workbook. A CSV has no hidden rows, so every
row gets followed up — including the ones deliberately filtered out. The page
warns about this; do not weaken it.

## Why there are no dependencies

Every library this used was replaced, and not for taste. The deliverable has to
reach a locked-down machine where email is the only route left, and each
dependency broke that in turn:

- **chromedp** shipped ten JavaScript files it pulled in with `//go:embed`. A
  mail gateway will not carry a `.js` file even inside an archive, so the
  bundle could not be sent at all. `internal/cdp` replaced it.
- **gobwas/ws** pulled in `gobwas/pool`, which references `golang.org/x/sys`;
  vendoring that dragged 7.2 MB of *Unix* syscall tables into a Windows-only
  tool. `internal/cdp/websocket.go` replaced it.
- **golang.org/x/sys** was also linked for a single registry read. PowerShell
  already answers the file-picker and stale-profile questions, so it answers
  the date-format one too.
- **excelize**, with `x/text`, `x/net` and `x/crypto` behind it, came to about
  10 MB for reading a few columns and writing a coloured copy back.
  `internal/xlsx` does that with `archive/zip` and `encoding/xml`.
- **x/text** was also decoding Windows-1252. That is a 32-entry table; the rest
  of the codepage is Latin-1.

The bundle went from 3.9 MB, refused with `550 Message Size Violation`, to
110 KB.

**Before adding a dependency, check two things:** does it bring any file type a
mail gateway treats as executable (`.js`, `.exe`, `.bat`, `.ps1`), and what
does it weigh once vendored? Either can break delivery without anyone noticing
until the next attempt fails.

`internal/cdp` implements only what this tool needs: launch Chrome, attach to
the page's own WebSocket endpoint, evaluate JavaScript, send keys and clicks,
take a screenshot. Attaching straight to the page means every command is
page-scoped, with no session ids to thread through.

`internal/xlsx` is cross-checked against **openpyxl** - an independent
implementation - to confirm the colours, frozen header, autofilter and column
widths survive into something Excel will actually open.

## Two lessons worth keeping

**A mistyped constant can look like a protocol problem.** The WebSocket
handshake failed for hours because the RFC 6455 GUID was written
`95CA-5AB0DC85B11F` instead of `95CA-C5AB0DC85B11`. The symptom gives nothing
away: the server simply answers for a key you did not send. When a handshake
"fails verification", check the constant before the logic.

**A skipped test is not a passing test.** That bug survived because the browser
tests skipped when Chrome could not be driven, and a skipped suite still
reports `ok`. Every browser test had been skipping. They now fail unless the
machine genuinely has no browser at all - `TestMain` and the `cdp` helper both
check `FindChrome()` before allowing a skip. Do not loosen that.

## Browser-layer gotchas — all three cost real debugging time

- **Element refs are stamped once and reused.** `page.go` gives each element a
  `data-socfu-ref`. Re-stamping on every query invalidated handles a caller was
  still holding, and `chromedp.Click` then waited *forever* for an element that
  no longer existed. Every call into Chrome also carries a deadline for the
  same reason.
- **The browser's lifetime is tied to the context of the first `Run`.**
  Wrapping that first call in a timeout context tears Chrome down the moment it
  returns. `Launch` therefore allocates on the page's own context and applies
  the deadline by *waiting*, not by cancelling.
- **Clearing a field means the value PROPERTY, not the attribute.** Setting
  the attribute leaves the live value in place on a field the user has typed
  into, and the next keystrokes are appended - so `SOC610529` + `SOC700001`
  became `SOC610529SOC700001`, which matches the *old* case. `clearField`
  goes through the prototype's native setter and dispatches an input event;
  `Type` then reads the value back and refuses to search if it did not land.
- **Clicks are dispatched at viewport coordinates**, so `Click` scrolls the
  element into view before reading its rectangle. The DOM `.click()` route is
  the fallback, not the default: OWA listens for real mouse events.

## Other things worth knowing

- The tracker binds to `127.0.0.1` only and every request needs a token minted
  at startup. The page can cause mail to be sent, so another local process must
  not be able to drive it.
- Auto-send needs a configured sender **and** an explicit acknowledgement, every
  run, not once.
- `releaseStaleProfile` kills only Chrome processes whose command line names
  *this* profile directory. Never loosen that to `chrome.exe` — the analyst's
  own browser must not be touched. It matches with `Contains` rather than a
  wildcard so a path holding `[ ] * ?` cannot widen it.
- The signed-in session lives in `uc_profile/` — treat it as credential
  material. Gitignored, as is `soc_settings.json` (holds the site-specific
  auto-send sender; never commit a real address).
- Test data uses fictional names and `@example.com` only. Keep it that way.

## Not done

- A run against a real corporate mailbox. Everything is proven against the
  stand-in one and against the Python original's recorded answers.
- The tool has never been through the target machine's application
  allowlisting. Building from source on that machine is the intended route.

## The browser is evidence

A run only closes the browser when it finished with nothing to inspect. A run
that errored, stopped, or left a case with no matching mail calls
`page.Leave()` instead, which drops the CDP connection and leaves the window
up. This exists because a failing run used to close the browser instantly and
take the only record of what it saw with it.

`Options.Unattended` (the self-test) and `Headless` always close - there is no
window for anyone to read.

## Testing: what exists, how to run it, and how to add to it

Read this before changing anything. Every layer below exists because
something broke that the layer above it could not catch.

### The four levels

| Level | Command | Needs | Catches |
|---|---|---|---|
| Offline suites | `go test ./... -count=1` (~12 min) | nothing (a headless browser is driven against a fake mailbox) | logic, matching, auto-send, the run loop, the tracker, xlsx |
| Self-test | `socfollowup.exe --self-test` | Chrome | the whole tool end to end, in one command |
| Live diagnostic | `socfollowup.exe --diagnose SOC123456` | a real signed-in mailbox | which link in the chain breaks on THIS OWA build; sends nothing |
| Supervised run | `socfollowup.exe --csv one_case.csv --verbose` | a real mailbox with matching mail | everything, with a human at the keyboard |

Run the first two after any change. Run the third when a real mailbox
behaves in a way the fakes do not.

### The rule that matters

**A test that cannot fail is worse than no test**, because it reads as
coverage. A test that cannot FINISH is the same problem wearing a different
hat: it does not fail, it hangs until Go's ten-minute panic, which stalls the
whole suite and reads as "still running" rather than "broken". Anything that
waits on stdin, a channel or a browser gets a bounded deadline - see `within`
in `cmd/socfollowup/ui_test.go`.

This has bitten three times:

- a `TestMain` calling `os.Exit(0)` plus a `t.Skipf` meant every browser
  test skipped while the suite printed `ok`, hiding a wrong protocol
  constant;
- `TestWildcardsInAPathDoNotWidenTheMatch` called `MkdirAll` on a path
  containing `*`, which is illegal on Windows, so it skipped on every
  platform it compiles for;
- a stdin test built on an `io.Pipe` that was never closed hung for ten
  minutes instead of failing, and an onboarding review reported the whole
  suite as broken.

And one more, which no rule about skips would have caught: a fix was applied
to `cmd/socfollowup/ui.go` by adding helper methods, while `Ask` and
`AskOrWatch` went on calling the old code. Go does not complain about an
unused method, so the build stayed green and the bug was reported fixed. **If
you fix something, run the thing that exercises it** - not just the build.

So: **`go test ./... -v | grep -c "^--- SKIP"` must print 0.** If you add a
skip, it must be for a machine that genuinely cannot run the test (no
browser at all), never for a condition that is normally true.

And when you fix a bug, **prove the test fails first**: revert the fix,
watch the new test go red, restore the fix. Several "regression tests"
written here passed against the broken code until that was checked.

### What the fakes can and cannot tell you

`internal/fakeowa/` is a stand-in Outlook. `Start()` gives an empty
mailbox; `StartWithSent(labels)` gives one whose Sent Items already holds
replies - the state a mailbox is really in on the second night, and the
one that made the worst bug reachable.

It is faithful about: message-list roles, the search box, the suggestion
popup, Reply all, compose, Sent Items, the selected-folder marker.

It is **not** faithful about the things that have actually broken live:

- Outlook prefixes every folder in the nav with a private-use icon glyph,
  so the entry reads `" Sent Items"`. Exact-match lookups miss it.
  Compare through `core.NormalizeText`, always.
- The search box is a controlled component and is re-created as you type.
  Text lands partially or not at all, which is why `RunSearch` types via
  `typeSearch`, retrying with a freshly located box. The read-back check
  in `Page.Type` stays strict - it is what stops the previous case's
  number being searched for.
- A row label is the whole row run together: sender, subject, preview AND
  the received time, e.g. `"... SOC 700020 Suspicious login Mon 8/10 No
  preview is available"`. The timestamp is in the MIDDLE, not the tail.
- `SOC123456` can find nothing while `SOC 123456` finds the mail. Both
  spellings are tried for that reason.

If a change depends on any of these, the offline suite will not tell you
whether you got it right. Use `--diagnose` against a real mailbox.

### Adding a test

Put it beside the code it guards, name it after the behaviour rather than
the function (`TestLastNightsReplyDoesNotConfirmTonightsSend`), and write
the comment as *why this exists* - the failure it prevents, in terms of
what the analyst would have seen. `internal/engine/audit_regression_test.go`
and `internal/core/rowtime_audit_test.go` are the models.

The arbiter for matching, sender parsing, time ordering and auto-send is
`internal/core/testdata/python_truth.json` - 97 answers from the original
Python. If a change makes `conformance_test.go` fail, the change is wrong
until argued otherwise in writing. There is exactly one deliberate
divergence and `conformance_test.go` applies it in the open.

### AUDIT.md

An adversarial audit (eight auditors, each finding handed to a skeptic
whose job was to refute it) produced 37 confirmed defects. All are fixed;
`AUDIT.md` records each one, what it would have cost the analyst, and how
it was fixed. Read it before concluding the code is fine - it is a list of
the mistakes this codebase actually attracts.

## Regenerating the arbiter

`internal/core/testdata/python_truth.json` holds 97 answers from the Python
original. `conformance_test.go` asserts against it, and when they disagree the
**code** is wrong, not the fixture.

Regenerating it used to be a documented step nobody could perform - the
generator was not in this repository and no file said where the original lived.
It is `tools/gen_truth.py` now:

```
python tools/gen_truth.py <path to the Python soc_followup.py> --check
```

`--check` recomputes every answer and reports differences **without writing**.
Run that first. If it reports nothing, the Go port and the Python still agree
and there is nothing to do.

It keeps every input in the fixture and only recomputes the answers, so it
cannot invent cases and cannot be used to make a failing Go test pass. Drop
`--check` to write, and say in the commit message what changed in the Python
and why regenerating was right.

Verified on 2026-08-22: the Python original reproduces the current fixture
exactly, all 8 sections.
