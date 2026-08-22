# DESIGN — what this tool is, how it should behave, and where each part lives

This is the specification. `CLAUDE.md` is the short orientation for someone
about to change the code; this file is the longer statement of what the tool
is *for* and what it must do. If the two ever disagree, this file is the
intent and the code is the evidence — reconcile them rather than picking one.

---

## 0. If you are Claude and you just opened this repository

You have everything you need in this repository. There is no missing context
and no private service — but **you need a Go toolchain, and on a locked-down
machine it is usually not on PATH.** Every command in every document here
starts with `go`, so find it before anything else:

```
where go                                     rem usually finds nothing
dir C:\Users\%USERNAME%\go-toolchain\go\bin\go.exe
dir "C:\Program Files\Go\bin\go.exe"
dir %LOCALAPPDATA%\Programs\Go\bin\go.exe
```

A portable toolchain unzipped anywhere works — Go needs no installer and no
registry entry. Put its `bin` on PATH for the session and carry on. If there is
no toolchain on the machine at all, that is a transfer problem rather than a
code problem, and `DEPLOYING.md` covers it.

**Do this first, in this order:**

1. Read §2 (the intended run) and §5 (auto-send). Those two decide whether any
   change is acceptable.
2. Run the tests. They need no mailbox, no network, no credentials:
   ```
   go build ./... && go vet ./... && go test ./... -count=1
   ```
   **About twelve minutes**, and mostly silent - it launches dozens of real
   headless browsers, and `internal/engine` alone takes ten of those minutes
   because it runs a full 30-case shift. It has not hung; let it finish.
   Everything runs against a fake Outlook served from `internal/fakeowa/`.
3. Skim §6, the question-to-file map. Do not read the whole tree — this is a
   small codebase but the interesting logic is concentrated in
   `internal/core/` and `internal/browser/outlook.go`.

**What this project is.** A single Windows `.exe`, written in Go, with **zero
third-party dependencies** — no chromedp, no excelize, not even
`golang.org/x/*`. It drives Chrome or Edge by speaking the DevTools Protocol
over a WebSocket client written by hand (`internal/cdp/websocket.go`), and it
reads and writes `.xlsx` by hand (`internal/xlsx/`). That is not an
architectural flourish: the machine it runs on has no module proxy, no PyPI,
no driver downloads, and may block GitHub. **Do not add a dependency.** If you
believe you need one, you need to write it instead.

**Build it:**
```
go build -ldflags="-s -w" -trimpath -o bin/socfollowup.exe ./cmd/socfollowup
```
`GOTOOLCHAIN=local` and `GOFLAGS` unset. Do not run `go mod tidy` — it reaches
for test-only modules that `go build` never needs, and the network it will try
to use is not there.

**Prove it still works, end to end, in one command:**
```
bin/socfollowup.exe --self-test
```
This drives the whole tool against a stand-in mailbox and checks four things:
it auto-sends when the newest mail really is from the expected sender, it
refuses when that sender is only quoted, it flags a case with no mail, and it
writes a colour-coded review sheet. If this passes, the tool works on that
machine. It sends no mail anywhere.

**The two things most likely to trip you up here:**

- **Bash heredocs in this environment mangle backslashes** (`\\` → `\`, and
  `"\n"` becomes a real newline, which breaks Go string literals). Edit files
  with the editor tools, not with `python - <<'EOF'` blocks containing escapes.
  This has broken the build twice.
- **Never use PowerShell `Get-Content | Set-Content`** on these files. PS 5.1
  reads UTF-8 as ANSI and silently corrupted a control character in the test
  harness, which made every audit case fail for reasons that looked like logic
  bugs.

**How the two halves talk.** The engine never imports any UI code. It calls
five methods on a `UI` interface (`internal/engine/engine.go`): `Log`,
`CaseUpdate`, `Ask`, `AskOrWatch`, `StopRequested`. `ConsoleUI`
(`cmd/socfollowup/ui.go`) and the tracker's UI (`internal/web/`) implement it,
and the tests supply their own scripted one. Keep it that way — it is why the
whole run loop is testable without a browser window.

**Before you say you are done:** build, vet, full test suite, `--self-test`.
If you changed matching, sender parsing, time ordering or auto-send, the
conformance test against the Python original (§6) is the arbiter, not your
reasoning.

---

## 1. The job, in one paragraph

A SOC analyst finishes a night shift with a filtered XSOAR export: a list of
incident case numbers that still need a follow-up mail. Each one means the same
few actions in Outlook on the web — search the case number, open the newest
mail about it, click **Reply all**, write nothing new, send. Fifty cases is an
hour of clicking, and the failure mode is not "the tool got it wrong", it is
"the analyst believed a case was handled when it was not". So the tool does the
clicking and the analyst keeps the judgement.

---

## 2. The intended run, step by step

This is the flow the tool is built around. Anything that does not serve it is
out of scope.

1. **Analyst picks the export.** The `.xlsx` they filtered in Excel. Rows the
   filter hid are skipped — that is the point of using the workbook.
2. **Analyst presses Start.** Nothing else is configured in the normal case.
3. **For each case number, the tool searches Outlook** — `SOC610529`, then
   `SOC 610529`, then the bare `610529`. First search with a real match wins.
4. **It opens the newest matching mail**, by timestamp, and confirms the
   reading pane is showing that case before it touches anything.
5. **It clicks Reply all** and stops.
6. **The analyst reviews the draft in Outlook and hits Send there.** The tool
   is watching: when the draft closes it notices by itself. The analyst does
   not have to come back to the tracker and click anything.
7. **It confirms the reply in Sent Items**, waits **5 seconds**, and starts the
   next case.

Two deviations from that path:

- **Auto-send.** If, and only if, the analyst turned it on and the strict rule
  matches (§5), step 6 happens without them. Everything else is identical.
- **A pause the analyst has to answer.** No mail found, or a draft that closed
  without a matching reply appearing in Sent Items. Every pause offers a
  button that moves to the next case, so a run is never stuck on one row.

### What the analyst never has to do

- Type a case number.
- Decide which of several mails is the newest.
- Return to the tracker after sending — it advances on its own.
- Tidy up a browser left over from a previous run.

---

## 3. Everything the tool does, and why each part exists

### Reading the export

| Behaviour | Why |
|---|---|
| `.xlsx` and `.csv` both read | analysts have both |
| Hidden rows in an `.xlsx` are skipped | that *is* the analyst's filter |
| A `.csv` warns that no filter survived in it | a CSV silently contains the rows they filtered out, and following those up is worse than doing nothing |
| Case-number column auto-detected, overridable | exports vary between XSOAR versions |
| Duplicates kept but marked | the analyst should see the sheet had duplicates, not have them quietly dropped |
| **Check export** reports what a run *would* do, changing nothing | reading the sheet wrong should be discovered before the mail goes out |

### Finding the mail

| Behaviour | Why |
|---|---|
| Three searches: `SOC610529`, `SOC 610529`, `610529` | proven live: `SOC610454` found nothing while `SOC 610454` matched. The spaced form also finds `[SOC#610529]` and `SOC-610529`, because Outlook splits on punctuation |
| No `subject:` searches | they matched nothing in any live test, and being last they were the term left in the search box after a miss, which read as if the tool had searched wrongly |
| Digit-boundary matching | `610529` must not match `SOC1610529`, a different case |
| Results scoped to the message list | OWA's search suggestions use the same `role=listbox/option` markup, so an unscoped lookup reads the suggestion dropdown |
| Newest chosen **by timestamp**, never screen position | "show newest on top" is a per-mailbox setting and inverts the order |
| The result list is allowed to settle | results stream in; the first poll can miss a newer mail that is the one to answer |

### Replying

| Behaviour | Why |
|---|---|
| Reading pane must show *that* case first | the previous case's mail lingers on screen |
| Reply all found on the toolbar, then the `...` overflow | a narrow window hides it behind More actions |
| Draft-closed watcher | so the analyst sends in Outlook and never returns to the tracker |
| Sent Items confirmation | "the draft closed" is not the same as "the mail left" |

### Reporting

| Behaviour | Why |
|---|---|
| Green only when a reply was confirmed in Sent Items | green means stop thinking about this case; nothing weaker earns it |
| Amber "Replied (unconfirmed)" | the send could not be verified — honest, not green |
| Grey for duplicates, red for not-found and errors | |
| Results written even when the run crashes | an hour of a night shift can sit in those results |
| Every row carries a plain-English reason | a status with no reason is not reviewable |

### Running the browser

| Behaviour | Why |
|---|---|
| Chrome preferred, Edge used if Chrome is absent | Edge is Chromium and speaks the same protocol; on a locked-down desktop it may be what is there |
| Chrome searched every way before Edge is considered | Edge is registered in App Paths on *every* Windows machine, so a registry-first sweep would beat a perfectly good Chrome in Program Files |
| Startup logs which browser it is driving | so a surprising choice is visible, not guessed at |
| Signed-in session kept in `uc_profile/` | MFA once, not once per run |
| Stale-profile guard kills only processes whose command line names **this** profile, of **the browser actually launched** | the analyst's own browser, with their tabs and their work, must never be touched |
| The browser is left open when a run ends badly | that window is the evidence: the last search typed, the last mail opened |

---

## 4. The tracker window

A local page, served from the binary, opened in the analyst's default browser.
No internet, no assets on disk.

- **Run settings** — file picker (or drag the sheet anywhere onto the page),
  case-number column, mailbox URL, stop-after-N, timings, the auto-send block.
- **Check export** — reads the sheet and reports what a run would do.
- **Counters** — sent, needs follow-up, not found, errored.
- **Prompt panel** — appears only when the run is waiting, with the choices as
  buttons. Every pause has a button that moves to the next case.
- **Case table** — one row per case, colour-coded, with the reason.
- **Log** — the same lines the console prints.

### Reporting failures

A handler that cannot read a request says *why* — whether the body arrived
empty (something between the browser and the tool stripped it) or arrived
unparseable, with its length and beginning. The page shows whatever comes back
even when it is not JSON, because a proxy's error page is exactly the thing
worth seeing. A bare `400 bad request` tells the analyst nothing and cost a
diagnosis cycle.

---

### Reply all, not reply — and that is deliberate

The tool clicks **Reply all**, so every recipient on the thread is included.
That is Outlook's own button; this code never touches the recipient list.

**This is the correct behaviour for a SOC follow-up, not a hazard to be
narrowed.** A follow-up has to reach everyone already on the case - the
reporter, the queue, whoever was cc'd when it was escalated. Quietly changing
this to Reply would drop people off a thread they are meant to be on, and it
would do so invisibly. Do not "fix" it.

The consequence to keep in mind is scope: with auto-send on, the reply goes to
however many people are on that thread. That is the reason the strict rule and
the per-run acknowledgement exist, and the reason `ClickSend` re-reads the
draft's subject before sending.

Known and deliberately not fixed: `--send-test-mail` cannot find the Cc field
on live OWA, so it can only build single-recipient test threads. That limits
the test scaffolding, not the follow-up path - the analyst-facing flow never
composes a new mail.

## 5. Auto-send — the dangerous part

Auto-send puts mail in the world with no human check. **It must fail closed:
anything ambiguous hands the case back to the analyst.**

It sends only when *all* of these hold:

- auto-send is on, and both the expected sender and the required phrase are
  configured — an empty setting never counts as a match;
- the **newest message by timestamp** is from the expected sender;
- the sender comes from the **real sender line**, not from element text that
  may carry body content or a quoted `From:`;
- the body contains the required phrase;
- the sender survives normalisation and is at least 4 characters — Outlook
  wraps long addresses mid-word and mixes in private-use icon glyphs.

Each of these exists because it broke in testing. `SenderPart` drops
anything at or after a recipient or quoted-header marker and after the first
timestamp; a quoted `From:` used to pass as the sender.

---

## 6. Where to look — a map for whoever changes this next

Read `CLAUDE.md` first. Then, by question:

| Question | File | Start at |
|---|---|---|
| Which searches are tried, in what order? | `internal/core/cases.go` | `BuildQueries` |
| Why did case 610529 match / not match this mail? | `internal/core/cases.go` | `LabelMatchesCase` |
| Why did two strings not compare equal? | `internal/core/text.go` | `NormalizeText` |
| Who does the tool think sent this mail? | `internal/core/text.go` | `SenderPart` |
| Would this mail have been auto-sent? | `internal/core/autosend.go` | `ShouldAutoSend` |
| Which mail counts as newest? | `internal/core/timeparse.go` | `ParseRowTime`, `SortNewestFirst` |
| How is the export read? Hidden rows? | `internal/core/sheet.go`, `internal/xlsx/read.go` | `ExtractCases` |
| Which column holds the case numbers? | `internal/core/sheet.go` | `DetectCaseColumn` |
| Why is a row that colour? | `internal/core/review.go` | `WriteReviewSheet`, `StatusStyle` |
| What does the tool click in Outlook? | `internal/browser/outlook.go` | `RunSearch`, `ClickReplyAll`, `ReadLastMessage` |
| How does it know the analyst sent it? | `internal/browser/outlook.go` | `NewSentWatcher`, `VerifySent` |
| How does it talk to the browser? | `internal/cdp/` | `websocket.go` — a hand-written RFC 6455 client |
| Which browser gets driven, and why that one? | `internal/cdp/chrome_windows.go` | `FindBrowsers`, `FindChrome` |
| Whose browser may be killed? | `internal/browser/profile_windows.go` | `staleProfilePIDs` |
| The run loop, the pauses, the statuses | `internal/engine/engine.go` | `Run`, `ProcessCase` |
| The tracker page and its API | `internal/web/page.go`, `internal/web/server.go` | `indexHTML`, `readRequest` |
| A stand-in Outlook to test against | `internal/fakeowa/server.go` | |

`internal/web/page.go` is the entire tracker UI as one Go raw string. **Its
JavaScript must not contain a backtick** — a stray one ends the Go string.

### The arbiter

`internal/core/testdata/python_truth.json` holds 97 answers generated from the
original Python implementation. `conformance_test.go` asserts against it. If a
change to matching, sender parsing, time ordering or auto-send makes that test
fail, **the change is wrong** until argued otherwise in writing.

There is exactly one deliberate divergence: the two trailing `subject:`
queries. The truth file still records them; `conformance_test.go` drops them in
the open, and fails if they ever vanish from the file — so the record stays
honest.

### Before you call anything done

```
go build ./... && go vet ./... && go test ./... -count=1
socfollowup.exe --self-test
```

The self-test drives the whole tool against a stand-in mailbox: auto-send when
the sender is real, refusal when it is only quoted, a flagged case with no
mail, and a colour-coded review sheet. It needs no mailbox and no network.

---

## 7. House rules

- **Fail closed.** When something is ambiguous, hand it back. A case wrongly
  handed back costs a minute; a case wrongly reported green costs an incident.
- **Say why in the log.** A skipped case with no stated reason is a bug.
- **Reproduce it in a test first.** Every guard above is a bug that happened.
- **Never overstate a result.** Green is a promise.
- Never rewrite these files with PowerShell `Get-Content | Set-Content`: PS 5.1
  reads UTF-8 as ANSI and has silently corrupted a control character here.
- No new dependencies. The target network has no PyPI, no Go module proxy, and
  may block GitHub. This tool has zero third-party dependencies and that is a
  feature, not an accident.
