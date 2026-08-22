# Audit, 2026-08-21

An adversarial end-to-end audit: eight independent auditors, one per
subsystem, each finding then handed to a skeptic whose job was to REFUTE it.
37 findings survived refutation, 14 were refuted and discarded.

Four auditors independently reached the same line, `engine.go:211`, by
different routes. That is the finding to read first.

**Fixed since this report was written** (each with a regression test that was
confirmed to fail without the fix):

| # | Defect | Fix |
|---|---|---|
| 1 | `VerifySent` given the zero time on the manual path, so any older reply confirmed tonight's send | stamp `openedAt` before the analyst can send, and pass it |
| 2 | the verify prompt fell through to SENT for any unrecognised answer | explicit `default:` - no answer means no send |
| 2b | "yes I sent it" after Sent Items came back EMPTY was painted green | returns `SENT_UNVERIFIED`; amber no longer sniffed out of a detail string |
| 3 | a late click answered the NEXT prompt, including "was it sent?" | prompts carry a generation; stale answers are dropped and the buffer drained |
| 4 | `VerifySent` treated an unreadable row date as proof of a fresh send | an unreadable date skips the row |
| 6 | `AskOrWatch` leaked a stdin reader that ate the analyst's answer | one reader owns stdin; input is drained before each prompt |
| 7 | an unverified send showed as a green "Replied" on the live tracker | amber pill, own label, not counted as replied |
| 8 | `ReadLastMessage` dropped a message whose BODY said "[draft]" and judged on an older one | draft markers are tested against the header only |
| 9 | `ClickSend` sent when it could not read the draft subject | it now refuses - the guard failed open |

**Also fixed, in a second pass** (`ParseRowTime` was fixed differently from the
report's suggestion - see below):

| # | Defect | Fix |
|---|---|---|
| 5 | `Page.Click` reported success for a click that never landed, and its fallback could not fire | the hit target is confirmed with `elementFromPoint`; `ClickFolder` confirms the view actually changed |
| 9 | `ParseRowTime` read its date from anywhere in the row, so subject text re-dated the mail | a date counts only where Outlook writes one: after the weekday, beside the clock, or alone in the label |
| 10 | `ClickSend` could not read an inline reply's subject at all | it reads contenteditable fields too, and falls back to the open conversation; otherwise refuses |
| 11 | a same-minute tie was broken by screen position | a tie is ambiguity - the case is handed back |
| 12 | a whitespace-only `--auto-phrase` matched every mail | guarded after normalisation, like the sender |
| 13 | an out-of-shape case cell was reshaped into a different valid-looking number | exactly one run of digits, 5-8 long, or the cell is unreadable |
| 14 | `render()` threw on `cases:null`, blanking the tracker | the page tolerates it and the server never sends null |
| 15 | `/api/start` check-then-act let two runs start | the run is claimed under the same lock as the check |
| 16 | blank rows below the data made the case column undetectable | only populated rows count |
| 17 | a malformed cell reference killed the process | bounded, and reported as a damaged file |
| 18 | cells with no `r` attribute collapsed into column A | a running cursor gives them their real position |
| 19 | `/api/state` encoded the snapshot outside the mutex | encoded under the lock |
| 20 | the review sheet stated two reasons that were false | both now read what actually happened |
| 21 | `SendTestMail` reported success on a click, not a send | it waits for the draft to close |
| 22 | the default `--test-case` was 10 digits, outside the tool's own shape | six digits |
| 23 | `--diagnose` reached verdicts the production code contradicts | it waits as `RunSearch` waits and applies the same undated-row rule |
| 24 | two upload paths reported failures wrongly | the real error is shown; the temp copy is cleaned up |
| 25 | `TestWildcardsInAPathDoNotWidenTheMatch` always skipped | the impossible `MkdirAll` is gone; it runs |

**Where the fix differs from the report.** Finding 9 suggested reading the
timestamp from the tail of the label. A live diagnostic run against a real
mailbox shows the received time sits in the MIDDLE - `"... Mon 8/10 No preview
is available"` - so a tail-only read would have broken every real row. The
implemented rule anchors on Outlook's own formatting instead.

## Found by the supervised live run, after the audit

The audit could only see the code. A supervised run against a real mailbox
found three things no offline test could have:

| Defect | What it cost | Fix |
|---|---|---|
| `ClickFolder` matched the folder name exactly, but Outlook prefixes every nav entry with a private-use icon glyph (`" Sent Items"`) | **Sent Items was never reachable on a real mailbox**, so every send was reported "could not check" and a confirmed green was unreachable | compare through `NormalizeText`, like every other comparison in the project |
| The search box is re-created as you type, so text landed partially or not at all (`the field holds "0020", not "SOC 700020"`) | three failed searches in a row = **NOT FOUND for mail sitting in the mailbox** - the symptom originally reported from the SOC laptop | `typeSearch` retries with a freshly located box; the strict read-back stays |
| Chrome launched at a narrow default size | OWA collapses the folder list and hides Reply all behind the "..." menu | `--window-size=1500,950` |

The first is the same trap `NormalizeText` was written for, and this was
the one lookup not using it.

**The live run also confirmed finding 1 is real and fixed.** That mailbox's
Sent Items holds a reply for SOC700020 dated 10 August. With nothing sent
tonight, the run logged `ignoring an older reply for 700020 (2026-08-10)`,
reported `Nothing matching in Sent Items`, asked, and recorded SKIPPED. The
pre-fix code would have accepted that reply and painted the row green
"confirmed in Sent Items".

---

The rest of this file is the report as produced, unedited.

---

# SOC follow-up tool — audit report

25 defects, grouped where they share a root cause, ranked by cost to the analyst. Line numbers verified against the current tree.

---

## Tier 1 — a case reported green/handled when it was not

### 1. `VerifySent` is called with a zero `since` on the manual path — CRITICAL
**`internal/engine/engine.go:211`** (`switch browser.VerifySent(r.page, num, time.Time{}, r.log)`)
Root cause of four separately-reported findings (autosend / matching / engine / browser).

`VerifySent`'s age guard is gated on `if !since.IsZero()` (`internal/browser/outlook.go:514`), so the zero time disables it entirely: **any** row in the top 12 of Sent Items whose label names the case counts as proof. The auto-send path does it correctly (`sentAt := time.Now()`, engine.go:252/255); the manual path — the default path every run uses — does not. `VerifySent`'s own doc comment states the contract this breaks.

It is reached with no human assertion at all: `NewSentWatcher` (outlook.go:538-552) fires when a compose pane "was seen open and has since closed (**sent or discarded**)", so discarding the draft returns `"auto"` at engine.go:206 and drops straight into this call.

**Trigger:** case SOC610529 was replied to on an earlier run/shift (crash re-run, or the incident recurs on tonight's export), so Sent Items holds `RE: SOC610529 …`. Tonight the analyst opens the draft and discards it. Result: `StatusSent`, reason "Analyst sent it; reply confirmed in Sent Items", log line "Verified in Sent Items.", review-sheet fill `C6EFCE` green "Replied". Nothing left the mailbox. Demonstrated: same page, `since=zero` → `SentFound`; `since=now` → `SentMissing` + "ignoring an older reply".

**Fix:** stamp `repliedAt := time.Now()` where `ClickReplyAll` succeeds and pass it here instead of `time.Time{}`. The existing 5-minute slack covers clock skew.

### 2. The "was it sent?" prompt: fall-through to SENT, and SentMissing painted green — HIGH
**`internal/engine/engine.go:227-235`** (prompt at :227, `return core.StatusSent, label, …` at :233)
Two reported findings, one block.

- The switch on `second` has cases for `"s"` and `"r"` and **no `default`**, so every other value means "yes, it was sent". `consoleUI.Ask` returns `"q"` outside `choices` on stdin failure — its own comment says `// stdin closed - treat it as "stop", never as a blind yes` (`cmd/socfollowup/ui.go:36-42`). The first prompt lists `"q"`; this one does not, so `"q"` lands on the fall-through. Trigger: `--csv shift.xlsx < answers.txt` where the answers file runs short, or the console is closed mid-run. Verified: with EOF between the two prompts, a full headless run with nothing ever sent produced `status=SENT`, green.
- The affirmative return sets `Detail = label` with no marker, and amber is decided purely by `strings.Contains(r.Detail, "not verified")` (engine.go:446-448). So `SentUnknown` ("the check could not run") is amber, while `SentMissing` ("the folder was read and the reply is **not there**") is green. The evidence is inverted; DESIGN §3 says "Green only when a reply was confirmed in Sent Items … nothing weaker earns it."

**Fix:** add `default: return core.StatusSkipped, label + " (no answer)", …`, and return `core.StatusSentUnverified` from the affirmative branch directly instead of relying on the substring sniff.

### 3. Web prompt answers are buffered and silently answer the *next* prompt — HIGH
**`internal/web/server.go:300`** (`case s.answers <- body.Choice:` with `answers: make(chan string, 1)`, server.go:96)

The comment says "nothing is waiting; drop it rather than block the page", but with a 1-slot buffer the non-blocking send **succeeds** when nobody is waiting. Neither `Ask` (:147) nor `AskOrWatch` (:165) drains before showing a prompt, and `handleStart` (:486-490) resets `st`, `index`, `stop` — never `answers`. The page's prompt buttons are not disabled on click (unlike Browse at page.go:307 and Stop at :402), and `AskOrWatch`'s watcher ticks every 700 ms against a 500 ms poll, so a click after the engine moved on is routine.

**Trigger:** analyst presses Send in Outlook, the watcher auto-advances, they then click "Sent ✓" (or double-click it). The stale `""` is consumed by the verify prompt → green row with no human answer. A stale `""` consumed by the *next case's* review prompt is worse: the engine skips waiting entirely and runs `VerifySent` while that case's draft sits open and unsent. A stale `"q"` — including from a previous run — ends the next run at its first case. All three reproduced against the real package.

**Fix:** drain `s.answers` inside `setPrompt`, and tag each answer with the prompt's generation counter, rejecting mismatches.

### 4. `VerifySent` treats an unparseable row timestamp as confirmation — HIGH
**`internal/browser/outlook.go:513-521`**

When `core.ParseRowTime` returns `ok == false` there is no `continue`; control falls through to `return SentFound`. Ambiguity resolved as proof — the opposite of DESIGN §7 and of the repo's own `TestUnparseableLabelIsRefusedNotGuessed` principle. Unparseable labels are ordinary: `shortDateRE` matches the first `\d{1,2}/\d{1,2}` pair anywhere and returns, so `RE: SOC610529 50/50 split rule 4:00 PM` is `ok=false` despite a real clock time. `VisibleResults` only filters undated rows on the *last-resort* selector, so normal-path rows arrive undated unchecked.

**Trigger:** auto-send clicks Send but the mail stalls in the Outbox; a case-matching row with an unreadable stamp is in Sent Items → `SentFound` on attempt 1 → "Auto-sent … and confirmed in Sent Items", green. Demonstrated live: the same page returns `SentMissing` for a parseable old row and `SentFound` for the `50/50` one.

**Fix:** move the `continue` so a failed parse skips the row: `stamp, ok := …; if !ok || stamp.Before(since.Add(-5*time.Minute)) { continue }`.

### 5. `Page.Click` reports success for a click that never landed; its documented fallback is unreachable — HIGH
**`internal/browser/page.go:361`** (`rectJS` OK test is only `width>0 && height>0`; `if err == nil { return true }` before `ClickJS`)

`Input.dispatchMouseEvent` returns success for coordinates where nothing is, or where an overlay is on top. Verified against real headless Chrome: click under a full-viewport `position:fixed` veil → `true`, no handler fired; button at `top:-5000px` → `true`, no handler fired. Because `r.OK` is true, `ClickJS` never runs — the fallback whose comment says it exists for "OWA can move a node between the hit test and the click" cannot fire in that case.

Every other `Click` caller is caught downstream (`WaitForReadingPane`, `Type`'s read-back, auto-send's `VerifySent(sentAt)`). The uncaught one is `ClickFolder` inside `VerifySent` (`outlook.go:468` short-circuits `!p.Click(el) && !p.ClickJS(el)`): a swallowed click on "Sent Items" makes it return true with the folder unchanged, so `VerifySent` reads the case's own inbox search results — rows selected by `LabelMatchesCase` in the first place. Combined with finding 1's zero `since`: `SentFound`, green row. Reproduced end to end (3.0 s, page still showing "Inbox").

**Fix:** after `ClickAt`, confirm with `document.elementFromPoint` that the hit target is the element or a descendant; otherwise fall through to `ClickJS`. Separately, have `ClickFolder` confirm the view changed before `VerifySent` reads rows.

### 6. `consoleUI.AskOrWatch` leaks a stdin reader that eats the analyst's "s" — MEDIUM
**`cmd/socfollowup/ui.go:63`**

`AskOrWatch` can return `"auto"` (:89) while the goroutine started at :63 is still blocked in `u.in.ReadString`. Nothing cancels it, and `Ask` then reads the same `bufio.Reader` from the main goroutine — two concurrent readers, a genuine data race, one orphan per watcher-terminated case.

**Trigger:** watcher fires, Sent Items has no match, analyst types `s` ("no, it wasn't sent") — the orphan consumes it and drops it into a channel nobody receives from. Reproduced with an `io.Pipe`: `Ask` never saw the line. The analyst's next move is Enter, which is `""` = yes → green row via finding 2.

**Fix:** one long-lived reader goroutine per `consoleUI` feeding a channel; `Ask` and `AskOrWatch` both receive from it.

### 7. Unverified sends show as green "Replied" on the live tracker — MEDIUM
**`internal/engine/engine.go:361`** (`ui.CaseUpdate(num, status, detail)` with the raw status)

`core.StatusSentUnverified` is produced in exactly one place — inside `writeOutputs` (engine.go:446-448) while building the review sheet. The front-end never sees it: `internal/web/page.go:105` has `.s-SENT` green with no unverified rule, `statusText` maps SENT → "Replied" (page.go:408), and `recount` adds it to `Replied` (server.go:135-136). During the shift, and in the end-of-shift counter, an unconfirmed send is indistinguishable from a confirmed one.

**Fix:** decide the unverified status in `ProcessCase`/`autoSend` and pass it to `CaseUpdate`; add the `.s-SENT_UNVERIFIED` style, `statusText` entry and recount bucket.

---

## Tier 2 — a reply sent on the wrong case, or sent when it should not have been

### 8. `ReadLastMessage` silently drops a "draft-looking" newest message and judges auto-send on an older one — HIGH
**`internal/browser/outlook.go:311`** (`if core.LooksLikeDraft(text) { continue }`), with `DraftMarkers` at **`internal/core/text.go:10`**

`LooksLikeDraft` lowercases and substring-matches the **whole element text** (header + body; `SenderPart`'s own comment confirms the body is in there) against `{"[draft]", "this message hasn't been sent", "saved:"}`. Losing the newest message is not treated as ambiguity — the decision is handed to an older one, and the oldest message in a SOC thread is the original alert, which always matches the rule. Same fail-open shape on the `!MsgTimeRE.MatchString` skip one line above.

**Trigger:** thread SOC700001 — msg 1 is the alert from the configured auto-send sender ("please follow up"); msg 2 is a newer human reply from `mallory@evil.example.com` whose body says `Evidence saved: \\soc\cases\700001`. Reproduced in a browser: `header` came back as the alert's sender line and `ShouldAutoSend` returned true. "Report Saved:" / "Ticket has been saved:" match too.

**Fix:** don't `continue` — record that a candidate was dropped and, if any was, return no header so `ShouldAutoSend` hands the case back. Anchor the markers to the header portion rather than the whole text.

### 9. `ParseRowTime` reads its timestamp from anywhere in the row label — HIGH
**`internal/core/timeparse.go:111`** onwards

The label is the whole OWA row (`aria-label || innerText`: sender + subject + preview + received time). Every pattern is matched against all of it, first match wins, and `todayRE`/`yesterdayRE` are checked **before** the real date. Measured at `now = 2026-08-09 20:00`:

| label | parsed |
|---|---|
| `… SOC610529 Today's shift summary Wed 8/5/2026 1:10 PM` | 2026-08-09 13:10 (3 months out) |
| `… recap of yesterday Wed 8/5/2026 1:10 PM` | 2026-08-08 13:10 |
| `… 24/7 monitoring alert 3:15 PM` | 2026-07-24 15:15 |
| `… blocked 10/10 attempts 4:00 PM` | 2025-10-10 16:00 |
| `… 50/50 split rule 4:00 PM` | **unparseable** |

`SortNewestFirst` then puts the wrong mail first and `ProcessCase` opens `ordered[0]` while logging "replying to the most recent one". Reproduced through a real browser against the project's own fake OWA. This is the same class the file already documents (the `weekdayRE` `\w*` fix), unbounded. `WaitForReadingPane` only asserts the right *case*, never the right *mail within* it. With auto-send on, the rule is then judged against the wrong message. If *every* matching row is unparseable, `SortNewestFirst` falls back to DOM order — the inversion DESIGN §3 forbids.

**Fix:** extract the timestamp from the trailing timestamp field only (the tail of the label), and check `fullDateRE`/`shortDateRE` before `todayRE`/`yesterdayRE`; when two patterns disagree, return `ok=false`.

### 10. `ClickSend`'s subject guard fails open when the subject cannot be read — MEDIUM
**`internal/browser/outlook.go:405`** (two reported findings, same branch)

`if subject != "" && !LabelMatchesCase(...)` refuses only a *readable, wrong* subject; `if subject == "" { log.say(…) }` then falls through to `p.Click(p.FindFirst(sendSelectors...))`. `page.go:519-520` names this check as the whole defence: "What stops a wrong compose being sent is not this - it is ClickSend, which re-reads the draft's subject and refuses if it is not the expected case." It is skipped in the one situation it cannot verify.

Reachable by construction: `compose.go:48-53` fills the subject via selectors that include any element (`[aria-label*='Add a subject' i]`), while `ComposeSubject` (`outlook.go:378-379`) reads only `<input>` variants. An inline reply-all — the auto-send path — shows no subject field at all, so `ComposeSubject` returns `""`. Reproduced: subject in `div[role='textbox'][aria-label='Add a subject']` holding case 610529, `ClickSend(p, "700001")` returned true and the page recorded the send; with two subject-less composes on the page the click landed on the *stale* draft (Send is chosen by document order). The only trace goes to `r.log`, silent without `--verbose`.

**Fix:** `if subject == "" { log.say(…); return false }`, and align `ComposeSubject`'s selector list with `subjectFieldSelectors`.

### 11. A same-minute tie is broken by screen position — LOW
**`internal/browser/outlook.go:348`**

`ParseRowTime` resolves to the minute, so any two messages in the same minute tie, and the tie-break takes the largest `y` — the bottom-most. The comment fourteen lines above says the bottom-most is the OLDEST under "Show newest messages on top". Reproduced: newest-first DOM, human reply and alert both at 6:30 PM → `header` = the alert → `ShouldAutoSend` true. Correct under the default setting, wrong under the inverted one; either way a tie is ambiguity that DESIGN §7 says must hand the case back.

**Fix:** on `len(ties) > 1`, return no header.

### 12. A whitespace-only `--auto-phrase` satisfies the body condition for every message — LOW
**`internal/core/autosend.go:30`**

`keyword == ""` is rejected *before* normalisation but matched *after*: `NormalizeText(" ")` is `""` and `strings.Contains(x, "")` is always true. The expected sender has the analogous post-normalisation guard (`len(normSender) < 4`); the phrase has none. Verified: `ShouldAutoSend("SOC Alerts <alerts@example.com>", "SOC700001 alert. Host quarantined. Nothing else.", " ", "alerts@example.com") = true`, reason `matched ' '` — the log even reports it as a match. Only reachable via an explicit `--auto-phrase " "`; `soc_settings.json` is saved by `firstNonEmpty`'s `TrimSpace` (main.go:69) and the web path trims (server.go:499).

**Fix:** guard after normalisation, mirroring the sender: `if len(NormalizeText(keyword)) < 3 { return false, … }`.

### 13. An out-of-format case cell is reshaped into a different, valid-looking case number — MEDIUM
**`internal/core/sheet.go:330`** (`m := CaseNumRE.FindString(raw)`, `CaseNumRE = \d{5,8}`)

`FindString` returns the first 5-8 digit run anywhere in the cell and never checks it is the whole token, so `123456789` → `12345678`, `INC0000610529` → `00006105`, an exponent-formatted cell `1.2345678E7` → `2345678`. `m == ""` never fires, so `stats.Unreadable` stays empty and Preflight reports "Cases to process: 2 / First few: SOC12345678" — breaking the file's own contract at sheet.go:26-28 ("a case that vanishes silently is a case nobody follows up on"). The tool then searches for a case number the analyst never wrote; `LabelMatchesCase` will accept an unrelated mail carrying it. The variant with no mitigation: two 9-digit ids sharing 8 leading digits collapse into one case, and the second row is painted grey "Same case appears earlier in the sheet - handled there" — a case believed covered that was never touched.

**Fix:** require whole-token match — `(?:^|\D)(\d{5,8})(?:\D|$)` — and file anything else under `stats.Unreadable`.

---

## Tier 3 — a run that loses results or cannot complete

### 14. `render()` throws on `"cases":null`, blanking the tracker — HIGH
**`internal/web/page.go:416`** (`st.cases.length` is the first statement)

`handleStart` sets `state{Running: true, Total: len(cases)}` with a nil `Cases` (server.go:487), which JSON-encodes as `null`; the first `CaseUpdate` only happens after `browser.Launch` and `WaitForMailbox` (up to 30 minutes of manual sign-in). `null.length` throws, `poll()`'s empty `.catch` (page.go:493) swallows it, and the settings form is already hidden (page.go:394). Nothing draws — not the counters, not the log, not `st.error`.

Confirmed against the real server plus the extracted script under node: with a bad ChromePath the state settles permanently at `{"running":false,"done":true,"error":"starting Chrome: fork/exec …","cases":null,…}` and `render()` throws forever. `runWindow` (main.go:477) prints only the URL, so there is no surviving feedback path — the analyst sees three empty panels and no sign the run died.

**Fix:** `st.cases = st.cases || []` at the top of `render`, or initialise `Cases: []caseRow{}` in `handleStart`.

### 15. `/api/start` is check-then-act — two concurrent starts both accepted — HIGH
**`internal/web/server.go:447-490`**

The mutex is released between testing `Running` (:448) and setting it (:487), with `core.ExtractCases` in between. The page's Start button is never disabled (page.go:376, unlike Browse and Stop). Two accepted starts means: two `engine.Run` goroutines on the same `ProfileDir` (the second's `releaseStaleProfile` kills the first's Chrome mid-case), `s.st = state{…}` erases the first run's rows, both share `s.answers`, and `run()` (:521) unconditionally sets `Done: true` — so the page shows "finished" with result files listed while the second run is still driving Outlook and can still auto-send. Two concurrent POSTs returned `[200 200]`. Window measured at ~20-69 ms for a 300-row .xlsx, so it needs a near-simultaneous pair or a slow sheet (network share, OneDrive-on-demand), not a routine double-click.

**Fix:** set `Running = true` under the same lock as the check, before `ExtractCases`, and clear it on the error paths.

### 16. Blank rows below the data sink `DetectCaseColumn` below its threshold — LOW
**`internal/core/sheet.go:252`** (`frac := float64(hits) / float64(total)`, `total = len(rows)`)

Excel keeps `<row>` elements for rows whose contents were deleted, and `readSheet` materialises every one. 50 real rows + 120 emptied gives `frac = 0.296`, just under the 0.3 hint-name threshold, so an unambiguous "Incident ID" column is rejected: "could not auto-detect the case-number column. Available columns: Incident ID, Name". Reproduced in both .xlsx and Excel-written .csv (trailing `,` lines). It fails closed (no wrong column can win) and the documented `--case-column` override works, but the error blames detection rather than saying the sheet is mostly empty, and `Preflight` returns on this error before it can print the blank-cell counts.

**Fix:** count only rows with at least one non-empty cell in the denominator.

### 17. A malformed cell reference kills the process — LOW
**`internal/xlsx/read.go:319`** (via `columnIndex`, read.go:435)

`col = col*26 + int(c-'A') + 1` has no bound, and the result is used directly as `make([]string, width)`. A 14-letter ref panics `makeslice: len out of range`; a 10-letter ref is worse — `fatal error: out of memory` (90 TB allocation), which no `recover` can catch, and there is no `recover` in the repo anyway. Exotic input (Excel's max column is `XFD`), and it dies in `ExtractCases` at startup before any mail is touched, so no results are lost.

**Fix:** reject `>3` letters / index `>16384` in `columnIndex` and return a worded error.

### 18. Cells written without an `r` attribute all collapse into column A — LOW
**`internal/xlsx/read.go:359`**

`r` is optional in spreadsheetml; when absent, `cellCol` stays 0 and every cell in the row overwrites `rowCells[0]`, so only the last cell survives. Reproduced: header `id,name` reads back as `["name"]`, case numbers gone, then "could not auto-detect the case-number column. Available columns: name". Silent — nothing in `Stats` records the loss, contradicting read.go:277-278 ("Row and column positions are honoured rather than assumed"). Every writer in this workflow emits `r`, which is the only thing keeping it low.

**Fix:** when `r` is absent, use a running cursor (previous column + 1).

---

## Tier 4 — everything else

### 19. `/api/state` encodes the snapshot outside the mutex — MEDIUM
**`internal/web/server.go:283-288`.** `snapshot := s.st` copies a slice *header*; `CaseUpdate` writes `s.st.Cases[i].Status/.Detail` into the same backing array while the encoder reads it. Unsynchronised string-header read/write — a torn header can fault the handler. Reproduced without `-race`: `/api/state` served `status="ERROR" detail="auto-sent and confirmed"`, a pairing no writer ever wrote. Transient only (re-render every 500 ms; the CSV and review sheet come from `Summary.Results`), so it cannot produce a persistent false green. **Fix:** encode into a buffer while holding the lock, or deep-copy `Cases`.

### 20. The review sheet states two reasons that are false — LOW (one root cause: reasons never consult what actually happened)
**`internal/core/review.go:105`** — `StatusDuplicate` always says "Same case appears earlier in the sheet - handled there", never consulting `statusByCase[num]`. When the first occurrence errored or was never reached, that is an assertion of a follow-up that never happened.
**`internal/core/review.go:107`** — rows whose case cell holds no `\d{5,8}` (blank, `pending assignment`, `NOPE`) keep `StatusPending` and get "The run ended before reaching this case". The run *did* reach them and deliberately dropped them into `stats.Blanks`/`Unreadable` — which the review sheet never surfaces, and which both entry points pass as `nil` during a real run (main.go:132, server.go:473). The analyst re-runs the sheet and the row is dropped again, identically, forever. Both reproduced against the real `WriteReviewSheet`. Neither paints green, so no case is promised handled.
**Fix:** pass the blank/unreadable line numbers into `WriteReviewSheet` and give them their own status and reason; make the duplicate reason read `statusByCase[num]`.

### 21. `SendTestMail` reports success on a Send *click* — LOW
**`internal/browser/compose.go:157-162`.** `ClickSend` + `Sleep(2s)` + `return nil`; nothing checks the compose closed or that anything reached Sent Items, and `runSendTestMail` (main.go:254-263) prints "Sent." on `err == nil`. Reproduced: a page whose Send leaves the draft open (what OWA does when a typed recipient never resolves) gives `err=<nil>, ComposeIsOpen=true`. Setup path only — costs one diagnosis round trip. `ComposeIsOpen` already exists one file over. **Fix:** poll `ComposeIsOpen` after the click and return the existing "did not send" error if it is still open.

### 22. The default `--test-case` is 10 digits — LOW
**`cmd/socfollowup/main.go:201`** (`time.Now().Format("0102150405")`). `CaseNumRE` is `\d{5,8}`, so the number cannot round-trip through the tool's own extractor: `0821212529` → `08212125` → `LabelMatchesCase("SOC0821212529 …", "08212125") = false`, i.e. red "no matching mail found" for a mail sitting in the inbox. Mitigated because the follow-up the CLI prints is `--diagnose`, which bypasses `CaseNumRE`. **Fix:** `time.Now().Format("150405")`.

### 23. `--diagnose` reaches verdicts the production code contradicts — MEDIUM / LOW (one root cause: the diagnostic models the search differently from `RunSearch`)
**`internal/browser/diagnose.go:72`** — the per-query sequence ends in a single `reportOneSearch` snapshot ~3.6 s after Enter, where `RunSearch` polls `VisibleResults` for 12 s with three allowed misses and an 800 ms re-check. On a page rendering results at 5 s, `RunSearch` returned 1 matching row while the report said "no rows matched ANY selector … The row markup is what needs looking at" — and searches 2 and 3 then reported search 1's late rows as their own and concluded "this search works". The opposite branch is equally wrong: a stale "We couldn't find anything" empty state yields "The mailbox genuinely has no mail matching this search - not a fault in the tool."
**`internal/browser/diagnose.go:222`** — `winnerCSS` is the first selector returning any rows, without `VisibleResults`' rule that the last-resort selector drops undated rows (outlook.go:108-129). On the repo's own `TestDiagnoseSpotsUnreadableTimestamps` page, `RunSearch` returns 0 rows while the report says "1 row(s) match, but 1 of them has no readable timestamp. The search is fine; the ORDER is not." That test asserts on the wrong wording, locking it in.
Diagnostic-only — no mail, no case status — but it points the developer at the wrong subsystem. **Fix:** have `Diagnose` call the same polling helper `RunSearch` uses before snapshotting, and apply the undated-row filter (or state it) in the `matched > 0` branch.

### 24. Two upload paths report failures wrongly — LOW
**`internal/web/page.go:341`** — the drop handler is the only page call that bypasses `apiRead`, and its `fetch(...).then(r => r.json().then(...))` chain has no `.catch` at any link, while `setupErr` was cleared at :340. If the tool is no longer listening (console closed), the drop is completely silent and the analyst re-drags the file. **Fix:** route it through `apiRead`, or add a `.catch`.
**`internal/web/server.go:387`** — every `ParseMultipartForm` failure is reported as "that file is too big to drop here - use Browse instead" and the real error is discarded. Verified: a truncated body and a missing boundary both produce that message. This is exactly what `readRequest` (server.go:343) was written to avoid. (Same handler: the `os.MkdirTemp` at :403 is never removed and `RemoveAll()` is never called, so each drop leaks a copy.) **Fix:** report `err`, and mention size only for `*http.MaxBytesError`.

### 25. `TestWildcardsInAPathDoNotWidenTheMatch` always skips — LOW
**`internal/browser/profile_windows_test.go:62`.** `filepath.Join(t.TempDir(), "uc[*]profile")` cannot be created on Windows and the file is `//go:build windows`, so the test skips on every platform it compiles for — the only SKIP among 96 tests. The property CLAUDE.md calls out ("matches with `Contains` … so a path holding `[ ] * ?` cannot widen it") has no live coverage. `staleProfilePIDs` never touches the filesystem. **Fix:** delete the `MkdirAll`.

---

## Verdict

> **This verdict is from BEFORE the fixes.** It is kept unedited as the record
> of what the audit concluded at the time. Every finding it tells you to fix -
> 4, 8, 9, 10, 11, and 1, 2, 3, 6 - is fixed, with a regression test that was
> confirmed to fail against the old code. See the tables at the top of this
> file. Do not read the paragraphs below as the current state of the tool.


**Auto-send ON — not safe to run unattended.** Three independent paths send or grade mail on a message the tool never correctly identified: `ReadLastMessage` silently drops the newest message on an ordinary English body containing "saved:" and judges the strict rule against the original alert (8); `ParseRowTime` re-dates rows from subject text, so the wrong mail in the thread is opened and judged (9); a same-minute tie is broken by the one signal the design forbids (11). The last guard between a wrong compose and the outbox — `ClickSend`'s subject check — is inert on exactly the inline reply-all that auto-send uses (10). And when the send fails, an unparseable Sent Items row confirms it anyway (4). Do not enable auto-send until 8, 9, 10, 11 and 4 are fixed.

**Auto-send OFF — the tool does not send mail on its own, but its output cannot be trusted, so it is not safe to leave unattended either.** Finding 1 is on the default path of every run: the zero `since` means a reply from any earlier run confirms tonight's draft, and the draft-closed watcher cannot tell Send from Discard — a discarded draft becomes a green "Replied" row with no human involvement. Finding 2 turns "the folder was read and the reply is not there" into green, and turns a lost stdin into a blind yes. Finding 3 lets a stale click answer a question nobody saw. Finding 6 swallows the analyst's "no". A green row here is not the promise DESIGN §3 makes.

Fix 1, 2 and 3 first — they are three small, local edits (pass a real `sentAt`; add the `default` and return `StatusSentUnverified`; drain the answer channel on `setPrompt`) and together they close every demonstrated path to a green row without a confirmed send.