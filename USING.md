# Using the follow-up helper

What it looks like and how to work through a shift with it. Written to be read
as text, since images and PDFs do not travel to the machines this runs on.

---

## What it actually does

For each case number in your filtered export, it searches Outlook on the web,
opens the newest matching mail, clicks **Reply all**, and waits for you to
review and send. It never types the reply for you.

At the end you get a results CSV and a colour-coded copy of your own sheet, so
you can see at a glance what still needs a human.

It can also send by itself, but only under a strict rule you configure, and
only if you turn it on. See [Auto-send](#auto-send).

---

## Starting it

Double-click `socfollowup.exe`. A console window opens and prints an address:

```
SOC night-shift follow-up

  Tracker: http://127.0.0.1:54873/?t=8f3c...

A browser tab should have opened. If it did not, paste that
address into your browser. Leave this window open while a run
is going - closing it stops the tool.
```

A tab opens in your **normal browser**. That is on purpose: a second Chrome
window opens separately to drive Outlook, and you should not be typing in it.

The tracker listens on `127.0.0.1` only, and the `?t=` token in the address is
checked on every request. Nothing outside your machine can reach it, and no
other program on your machine can drive it.

---

## The settings screen

```
 SOC night-shift follow-up            not started
 ---------------------------------------------------------------------------
  RUN SETTINGS

  Export to work through
  [ C:\Users\you\Downloads\shift.xlsx              ]  [ Browse... ]
  Use the .xlsx you filtered in Excel. Rows you filtered out are
  skipped, and the filter only survives inside a workbook.

  Case-number column      Mailbox URL              Stop after N cases
  [ auto              ]   [ https://outlook...  ]  [ 0            ]

  Search settle (secs)    Wait after a send (secs)
  [ 3                 ]   [ 5                   ]

  [ ] Don't stop when a case has no matching mail
  [ ] Send automatically when the rule below matches

  [ Start run ]  [ Check export first ]
 ---------------------------------------------------------------------------
```

You can also **drag the export anywhere onto the page** instead of browsing.

| Field | What it does |
|---|---|
| **Export to work through** | Your filtered XSOAR export. Use the `.xlsx`. |
| **Case-number column** | Leave blank and it works out which column holds the case numbers. Fill it in only if it guesses wrong. |
| **Mailbox URL** | Where Outlook lives. `https://outlook.office.com/mail/` for work, `https://outlook.live.com/mail/` for a personal account. |
| **Stop after N cases** | `0` means all of them. Set it to `1` or `2` the first time you run it. |
| **Search settle** | How long to let Outlook's search results settle before reading them. Raise it on a slow connection. |
| **Wait after a send** | A pause after each send before starting the next case. |
| **Don't stop when a case has no matching mail** | Off by default, so a missing case stops and asks you. Tick it for an unattended run. |

### Give it the `.xlsx`, not a CSV

Rows you filtered out in Excel are **skipped** — the tool respects your filter.
But a filter only exists inside a workbook. A `.csv` has no hidden rows, so
every row in it gets followed up, **including the ones you deliberately
filtered out**. If you pick one, the page says so in an amber box.

### Check export first

Reads the sheet and tells you what a run would do. **It does not touch
Outlook.** Always worth doing on a new export:

```
Case-number column : "Case ID"
Columns in file    : Case ID, Summary, Severity
Rows read          : 312
Filtered out (Excel): 47 row(s) skipped
Cases to process   : 260
Duplicates ignored : 5 (e.g. row 88: 610529)
!! NO CASE NUMBER FOUND in 2 row(s) - these will be IGNORED:
     row 141: 'n/a'
     row 209: 'pending triage'
First few          : SOC610529, SOC610530, SOC610531
```

Anything it cannot read is listed with its row number rather than quietly
dropped.

---

## Auto-send

Off unless you turn it on. Ticking it opens this:

```
  [x] Send automatically when the rule below matches

  Only if the NEWEST mail is from       and the mail contains
  [ alerts@example.com            ]     [ follow up            ]

  +---------------------------------------------------------------+
  | Auto-send replies with no review. The reply carries your      |
  | Outlook signature exactly as it is set right now, and it goes |
  | to everyone on the thread. Check your signature before you    |
  | turn this on.                                                 |
  +---------------------------------------------------------------+

  [ ] I have checked my Outlook signature and I understand
      replies will be sent without me seeing them.
```

**Both conditions must hold**, judged on the **newest message in the thread**:

1. it is from the sender you configured, and
2. the message contains your phrase

Anything ambiguous hands the case back to you. In particular it refuses when:

- the newest message only **quotes** that sender (a forward, or a reply that
  includes their earlier mail)
- the thread's messages cannot be put in time order
- the message or the sender line cannot be read
- the draft that opened is not for this case
- the configured sender is too short to match safely

The acknowledgement is required **every run**, not once. Both it and the sender
must be set or the run refuses to start.

---

## While it runs

```
 SOC night-shift follow-up            running
 ---------------------------------------------------------------------------
  +----------+  +----------+  +----------+  +-----------+  +----------+
  | 5 / 6    |  | 2        |  | 1        |  | 1         |  | 0        |
  | PROCESSED|  | REPLIED  |  | SKIPPED  |  | NOT FOUND |  | ERRORED  |
  +----------+  +----------+  +----------+  +-----------+  +----------+

  +-----------------------------------------------------------------------+
  | The reply-all draft is open in Outlook. Review it and hit Send there  |
  | - this updates on its own when the draft closes.                     |
  |                                                                       |
  | [ Sent OK ]  [ Skip ]  [ Retry ]  [ Stop run ]                        |
  +-----------------------------------------------------------------------+

  CASES
  CASE          STATUS            DETAIL
  SOC610529     Replied           Jordan Lee SOC610529 Suspicious login
  SOC610530     Replied           SOC Alerts SOC610530 Impossible travel
  SOC610531     Skipped           SOC Alerts SOC610531 Phishing report
  SOC610532     Not found         no mail matched (tried 3 searches)
  SOC610533     Waiting for you   SOC Alerts SOC610533 Malware alert

  LOG
  [5/6] SOC610533
      Match (1 result(s), query SOC610533):
      Reply-all draft is open in the browser. Review, edit, hit Send.

  [ Stop after this case ]
 ---------------------------------------------------------------------------
```

### What the statuses mean

| Status | Colour | Meaning |
|---|---|---|
| **Replied** | green | Sent, and confirmed in Sent Items. Done with. |
| **Skipped** | amber | You skipped it, or the draft closed without sending. Still needs you. |
| **Waiting for you** | amber | The draft is open in Outlook right now. |
| **Not found** | red | No mail in the mailbox mentions this case, after three different searches. |
| **ERROR** | red | Something went wrong — the detail says what. |
| **Working...** | blue | Currently being searched for. |
| **Pending** | grey | Not reached yet. |

Green means you can stop thinking about that case. Nothing else does.

---

## The normal loop

For each case:

1. It searches Outlook, trying `SOC610529`, then `SOC 610529`, then the bare
   number. Those three are all that get tried: `subject:` searches matched
   nothing in any live test, so they are not used.
2. If several mails match, it opens the **newest by timestamp** — not the one
   highest on screen, because "show newest on top" is a per-mailbox setting.
3. It checks the reading pane is really showing that case before touching
   anything.
4. It clicks **Reply all** and waits.
5. **You review the draft in Outlook and hit Send there.** The tracker notices
   the draft close on its own and moves on — you do not need to come back and
   click anything.
6. It checks Sent Items to confirm the reply actually left, then waits your
   configured delay and starts the next case.

You only need to touch the tracker when it asks you something.

---

## The three questions it can ask

**After a draft is open**

> The reply-all draft is open in Outlook. Review it and hit Send there — this
> updates on its own when the draft closes.
>
> `[ Sent OK ]` `[ Skip ]` `[ Retry ]` `[ Stop run ]`

Usually you ignore this and just send in Outlook. Use **Skip** to leave a case
for later, **Retry** to search again if something looked wrong.

**When nothing matched**

> Nothing in the mailbox matches this case. Carry on?
>
> `[ Continue ]` `[ Stop run ]`

Only appears if you left *Don't stop when a case has no matching mail*
unticked.

**When a send cannot be confirmed**

> The draft closed, but no matching reply turned up in Sent Items. Was it
> actually sent?
>
> `[ Yes, it was sent ]` `[ No — mark skipped ]` `[ Retry ]`

Answer honestly — this decides whether the row goes green or amber. If you say
yes but it was never confirmed, the sheet records exactly that rather than
claiming it was verified.

---

## What you get at the end

Two files, written beside the executable:

**`followup_results_<timestamp>.csv`** — one row per case: case, status, why,
detail.

**`followup_review_<timestamp>.xlsx`** — your own sheet back, with two columns
added and every row coloured:

| Your columns... | Follow-up status | Why |
|---|---|---|
| 610529, suspicious login | Replied | Auto-sent (matched 'follow up'; latest mail is from 'alerts@example.com') and confirmed in Sent Items |
| 610531, phishing report | Skipped | Draft was opened; the analyst skipped it without sending |
| 610532, malware alert | NOT FOUND | Searched 3 ways (SOC610532, SOC 610532, 610532) - no mail in this mailbox mentions 610532 |

Green rows are finished. Red rows need a human. Amber rows were started but not
confirmed. Grey rows are duplicates of a case handled further up. Yellow rows
were never reached.

**These are written even if the run crashes part way through.** An hour of a
night shift does not get thrown away because the browser died at case 200.

---

## Stopping

**Stop after this case** finishes what it is doing and then stops cleanly,
writing both files. Closing the console window also stops it, but do that only
if it is wedged.

---

## If something goes wrong

Run this first — it takes about fifteen seconds and touches nothing real:

```
socfollowup.exe --self-test
```

It drives a fake mailbox served from inside the program itself. If those checks
pass, the machine is fine and the problem is Outlook-side.

| Symptom | What it means |
|---|---|
| Sign-in page appears | Normal on first run. Sign in, tick *Stay signed in*, and it remembers next time. |
| "Signed out?" mid-run | The session dropped. Sign in again in the automated window; it waits up to 30 minutes. |
| A case goes red as **Not found** | There genuinely is no mail mentioning it. The Why column lists what it searched. |
| **Reply all button not found** | The mail opened but the button was not there or in the `...` menu. Reply to that one by hand. |
| Everything fails at launch | Another Chrome may be holding the profile. It tries to clear that itself, and only ever ends processes using *its own* profile, never your normal browser. |

The console window carries a fuller log than the page. Copy it out if you need
to send it on.

---

## Changing the UI (for whoever edits this next)

The whole interface is two files and no build step.

| File | Holds |
|---|---|
| `internal/web/page.go` | the entire page - HTML, CSS and JavaScript in one Go raw string, `indexHTML` |
| `internal/web/server.go` | the HTTP handlers, and the `engine.UI` implementation the run talks to |

Nothing is fetched from the internet and there is no asset directory: the page
is compiled into the binary. Open it with `socfollowup.exe --ui` (or just
double-click) and edit `page.go` to change it.

### How the page and the run talk

The page polls `GET /api/state` every 500 ms and re-renders from whatever comes
back. There are no websockets and no server-push - it is deliberately dull, so
a dropped request costs half a second and nothing else.

| Endpoint | Purpose |
|---|---|
| `GET /api/state` | everything the page draws: cases, tallies, log, current prompt, output files |
| `POST /api/start` | the settings form |
| `POST /api/answer` | `{"choice": ""}` - answers whatever the run is asking |
| `POST /api/stop` | stop after the current case |
| `POST /api/check` | preflight a sheet without touching Outlook |
| `POST /api/browse` | opens the native Windows file dialog, host-side |
| `POST /api/upload` | a file dragged onto the page |

Every one requires the token, as `X-Socfu-Token` or `?t=`.

### Adding a field

1. Add it to `startRequest` in `server.go` (for a setting) or to `state` (for
   something displayed), with a `json` tag.
2. Add the input to `indexHTML`, and read it in the `start` click handler.
3. If it is displayed, render it in `render()`.

### Things to leave alone

- **The JavaScript must not use backticks.** The page lives inside a Go raw
  string; one template literal ends the string and the file stops compiling.
- **The token check.** This page can cause mail to be sent, so another process
  on the machine must not be able to drive it.
- **Auto-send needs both a sender and the acknowledgement, every run.** Both
  are checked server-side in `handleStart`, not just in the page - a caller
  that skips the form still cannot skip the guard.
- **The CSV warning.** Handing the tool a `.csv` silently undoes the analyst's
  Excel filter. Weakening that warning re-opens a real failure.
- **`QUESTIONS` and `LABELS` in `page.go`** map a prompt kind to its wording
  and its buttons. The engine sends terminal-shaped prompts; the page is what
  turns them into something clickable. Add a kind to both maps together.

### Checking a change

`internal/web/server_test.go` drives the whole thing over real HTTP against a
stand-in mailbox: start a run, poll, answer the prompt, check the tallies and
the files that come out. It also proves an untokened request is refused and
that auto-send will not start without its acknowledgement.

```
go test ./internal/web/
```

To eyeball the page without a mailbox, run `socfollowup.exe --ui` and use
**Check export first** on any sheet - that path touches nothing.

---

## When the run ends, does the browser close?

Only if there was nothing left to look at. If the run finished with every case
replied to, it closes and says so.

If anything wants your eyes — a case with no matching mail, a case that
errored, or a run you stopped — it **leaves the browser open** and says so in
the log. The window is the evidence: it shows the last search it typed and the
last mail it opened. Close it yourself when you are done. You do not have to
tidy up before the next run — that one frees the profile by itself.

---

## Before your first run on a work mailbox

`DEPLOYING.md` has an ordered checklist - self-test, then `--diagnose` on that
tenant, then `--check` on your sheet, then one supervised case with auto-send
off. It also covers the part no software can settle for you: this tool signs in
as you and clicks Reply all, so a reply reaches everyone on the thread from
your address. Square that with the mailbox owner and your security team before
the first run, not after.

## The full list of command-line flags

The tracker window covers everything an analyst needs. These exist for the
console, for testing, and for the awkward cases.

| Flag | What it does |
|---|---|
| `--csv <file>` | the export to work through (`.xlsx` or `.csv`) |
| `--check` | read the export and report what a run would do, then stop |
| `--column <name>` | the case-number column, if detection picks wrong |
| `--url <address>` | the mailbox. Default is `outlook.office.com`; a personal account is `outlook.live.com` |
| `--limit <n>` | stop after n cases |
| `--no-pause` | do not stop on a case with no matching mail |
| `--settle <seconds>` | how long to let search results settle (default 3) |
| `--send-delay <seconds>` | wait after a send before the next case (default 5) |
| `--auto-send` | see the auto-send section. Off unless you ask. On the console it also asks you to type `YES` before the run starts - a script that does not answer will sit there, and one with no stdin prints `Stopped.` and exits **0** |
| `--auto-sender <address>` | required by `--auto-send` |
| `--auto-phrase <text>` | required phrase, default "follow up" |
| `--profile-dir <dir>` | where the signed-in session lives |
| `--out-dir <dir>` | where results are written |
| `--chrome <path>` | drive a particular browser (e.g. Edge) |
| `--headless` | no visible window. You cannot review drafts, so this is for testing |
| `--verbose` | print the browser-level log too |
| `--self-test` | prove the tool works here, against a stand-in mailbox |
| `--diagnose SOC123456` | see the next section |
| `--ui` | open the tracker window (the default when you double-click) |

### `--diagnose SOC123456` — when a case is not found but the mail is there

Searches your real mailbox for one case and reports which link in the chain
broke: the search box, the message list markup, the date format, or the mail
genuinely not being there. **It opens no mail and sends nothing.** It saves a
short text file you can send to whoever maintains this — it contains only the
row labels Outlook itself shows in the list, never message contents.

Run this first whenever a case comes back NOT FOUND and you can see the mail
in Outlook yourself.

### `--send-test-mail <address>` — this one sends real mail

**This is the only flag that puts a NEW mail into the world.** It exists so an
end-to-end check has something real to find, without waiting for a genuine
alert.

```
socfollowup.exe --send-test-mail you@example.com --test-cc colleague@example.com
```

It prints the recipients and the subject, and waits for you to type `yes`
before anything is sent. `--test-cc` adds a second recipient so Reply all has
someone to include. `--test-case <number>` sets the case number in the
subject; by default it makes a fresh one. `--yes` skips the confirmation —
**do not use it unless a script needs it**, because the confirmation is what
stands between a mistyped address and a stranger's inbox.

The mail says plainly that it is an automated test and asks the reader to
ignore it.

---

## What a sheet has to look like

`example_shift.csv` ships in the repo. A sheet needs one column of case
numbers; everything else is carried along and ignored:

```
Case ID,Summary,Severity
SOC610529,Suspicious login from unusual location,High
SOC700001,Repeated failed authentication,Medium
```

The rules:

- a case number is **5 to 8 digits**. `SOC610529`, `SOC 610529` and `610529`
  all read the same way;
- a cell with no case number, or with something that is not one number
  (`pending assignment`, `123456789`), is reported as unreadable rather than
  guessed at - run `--check` to see those before you start;
- the column is detected automatically; `--column` overrides it;
- in an `.xlsx`, rows your Excel filter hid are skipped. In a `.csv` they are
  not, because a CSV does not carry a filter.
