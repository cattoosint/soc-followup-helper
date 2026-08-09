# SOC Nightshift Follow-up Helper

Semi-automates the nightly follow-up loop: reads case numbers from the
(filtered) XSOAR export, then drives Outlook on the web — for each case it
searches the mailbox, opens the newest matching email, clicks **Reply all**,
and waits for the analyst to review and hit **Send** before moving to the
next case. The analyst stays in control of every email; the script only does
the searching, clicking and bookkeeping.

## What it does per case

1. Searches Outlook for the case, trying each form until one matches:
   `SOC610321` → `SOC 610321` → `SOC#610321` → `610321` → the `subject:`
   forms. The sheet only needs the bare number (e.g. `607503`); the subject
   can write it however the SOC does:

   `SOC607503` · `SOC 607503` · `SOC#607503` · `SOC-607503` ·
   `SOC_607503` · `SOC:607503` · `[SOC607503]` · `[SOC #607503]` ·
   `Case 607503` · with `RE:` / `FW:` / `[CLOSED]` prefixes

   A longer number that merely contains the case (`SOC#1607503`,
   `SOC#6075031`) is **not** treated as a match.
   Spacing matters, case doesn't — verified live: `SOC610454` found
   nothing while `SOC 610454` matched, and uppercase `SOC610529` matched a
   subject written `soc610529`.
2. Only rows whose text actually contains that case number count as a match,
   so a search that quietly returns the whole inbox can't be mistaken for a
   hit. Of those, it replies to the one with the **latest timestamp** — the
   times are read off the rows, so it doesn't matter whether Outlook sorted
   the results by date or by relevance
3. **Found** → opens the match, clicks *Reply all*, then pauses in the
   terminal until you've reviewed/edited/sent the reply — press **Enter** to
   continue to the next case (`s` skips it, `q` quits)
4. **Not found** → flags it in the console, pauses so you can check, and logs it

At the end it writes:

- `followup_results_<timestamp>.csv` — every case with its status
  (SENT / SKIPPED / NOT_FOUND / ERROR)
- `followup_review_<timestamp>.xlsx` — a copy of your sheet with a
  **Follow-up status** column and every row colour-coded:

  | Colour | Meaning |
  |---|---|
  | 🟩 green | Replied — done |
  | 🟥 red | NOT FOUND / ERROR — still needs a human |
  | 🟧 amber | Skipped by the analyst |
  | 🟨 yellow | Not done yet (run stopped before reaching it) |

  Hit **⬇ Review sheet** in the GUI to generate and open this at any time —
  including **mid-run**, so you can hand over or check progress without
  stopping. At the end of a run it pops up a summary listing everything
  that still needs follow-up.

## Setup (once per machine)

1. Install Python 3.10+ from python.org (tick "Add to PATH")
2. In a terminal: `pip install seleniumbase openpyxl`
3. Chrome must be installed. On the first web-mode run, SeleniumBase
   auto-downloads a matching chromedriver (needs internet once).

Web mode drives an **undetected Chrome** (SeleniumBase UC mode) — Microsoft's
sign-in risk engine was revoking sessions in visibly-automated browsers, so
the browser hides its automation fingerprint. This is the same engine the
SOC's phishing-analysis tooling uses.

## Running it — GUI (recommended)

Double-click `run_followup.bat` (or run `python soc_gui.py`). Then:

1. **Browse** to your filtered export — it immediately shows which column it
   detected, how many cases it will process, and lists them all in the
   tracker table as *Pending*. You can also paste a path into the box and
   press Enter (quotes from "Copy as path" are fine), drag the sheet onto
   `run_followup.bat`, or hit **Reload** after re-saving it in Excel. The
   sheet can stay open in Excel — it reads a copy if the file is locked.
2. Pick the **Mailbox** (work Outlook or a personal test one), then hit
   **Start**
3. Each case moves through the table live:
   *Pending → Searching… → Awaiting send → Sent ✓ / NOT FOUND ⚠ / Skipped*
4. When a draft is open, you review + Send in Outlook — the tool **detects
   the send automatically**, then **verifies the reply actually landed in
   Sent Items** before marking the case SENT. If the draft closed but
   nothing is in Sent Items (e.g. you discarded it), it asks you instead of
   guessing. The Sent ✓ / Skip / Retry / Stop buttons still work as a
   manual override.
5. After a send it waits 5 seconds (adjustable — **Wait after send** in the
   GUI) before searching for the next case, so Outlook has time to finish
   sending and settle
6. Not-found cases pause with a **Continue** button (untick *Pause on
   not-found* to keep going without stopping) and come out **red** in the
   review sheet either way — nothing is lost if you don't pause

## Running it — command line

```
python soc_followup.py --csv cases.xlsx
```

(or drag a CSV/XLSX onto `run_followup.bat`)

- Input can be `.csv` or `.xlsx`. The case-number column is auto-detected;
  override with `--column "Case ID"` if it guesses wrong.

## First time with a real XSOAR export

Before running a shift's export for the first time, dry-run it — this reads
the file only, nothing touches Outlook:

```
python soc_followup.py --csv shift_export.xlsx --check
```

or click **Check export** in the GUI. It reports the column it detected, how
many cases it will process, rows filtered out in Excel, duplicates, and —
most importantly — **any row whose case number it can't read**, with line
numbers, so nothing is silently dropped:

```
Case-number column : 'incident id'
Rows read          : 6
Cases to process   : 3
Duplicates ignored : 1 (e.g. row 4: 610321)
!! NO CASE NUMBER FOUND in 1 row(s) - these will be IGNORED:
     row 5: 'INC-99'
```

The sheet only needs the **bare number** as XSOAR exports it (`523423`) —
the `SOC` prefix is added by the tool when it searches. Numbers are read as
any 5–8 digit value in the chosen column, so `523423`, `SOC523423` and
`[SOC#523423]` in the sheet all work equally.

The check also warns if **row 1 looks like data rather than column
headings** — row 1 is always treated as headings, so without that warning
the first case would go missing from the run.

## Excel filters — important

An Excel filter only **hides** rows; the data stays in the file. This script
handles that: for `.xlsx` input, rows hidden by your filter are **skipped**
(it tells you how many). So the workflow is:

> filter in Excel → **save as .xlsx** → load it here → only visible rows run

Don't save the filtered sheet as **.csv** — CSV files can't remember filters,
so every row (including the filtered-out ones) would be processed.
`test_filtered.xlsx` in this folder is a demo file with 2 hidden rows you can
use to see the skipping work.
- **First run:** a browser window opens on the Outlook login page — sign in
  manually (MFA and all) and choose **Yes** at "Stay signed in?". The login
  is saved in the `uc_profile` folder and **survives closing the app and
  rebooting the PC** (Microsoft's tokens last about a year), so later runs
  go straight to the mailbox. Use the **Sign in to Outlook** button to do
  this once without starting a run.
- Useful options:
  - `--parse-only` — just show which case numbers it read from the file
  - `--limit 3` — only process the first 3 cases (good for testing)
  - `--no-pause` — don't stop on not-found cases, just flag and continue
  - `--url https://outlook.live.com/mail/` — use a personal outlook.com
    mailbox (for demos, see below)
  - `--settle 5` — wait longer after each search (slow network/mailbox)
  - `--send-delay 10` — pause longer after each sent reply (default 5s, 0 off)

## Demo / proof of concept (no work account needed)

You can demo the whole flow with a free personal outlook.com mailbox:

1. Send that mailbox a few test emails with subjects like
   `SOC 610321 - Suspicious login` and `SOC123021 phishing follow-up`
2. Put those numbers (plus one bogus number) in a CSV — `sample_cases.csv`
   in this folder matches those examples
3. Run:
   `python soc_followup.py --csv sample_cases.csv --url https://outlook.live.com/mail/`
4. Watch it find each case, open Reply all, wait for you to send, and flag
   the bogus number as NOT_FOUND with a highlighted row in the Excel output.

## Audit / self-test

```
python audit_simulation.py     # 6 scenarios + 11 behavioural checks
python test_edge_cases.py      # 14 edge cases
```

Runs the whole tool against a **simulated Outlook** — no mailbox, no
network — and checks every branch: reply-all via the toolbar button and via
the ⋯ menu, a discarded draft, a case with no matching mail, a search that
silently returns the wrong rows, two mails for one case (must reply to the
newest), Excel-filtered rows being skipped, the post-send pause, the results
CSV, and the red highlighting. The edge-case suite adds lookalike case
numbers (610529 must not match SOC**1**610529), Stop mid-run, Retry,
sign-out recovery, unverifiable sends, and odd spreadsheets. Both print a
pass/fail line per check and exit non-zero on failure — useful evidence when
proposing the tool, and a regression check after Microsoft changes the
Outlook web UI.

### Live probe (real mailbox, sends nothing)

```
python live_probe.py 610529
```

Runs every page interaction against the **real** Outlook web UI for one
case — search, result rows, timestamp parsing, opening the mail, Reply all,
compose detection, the Sent Items check — then discards the draft. Nothing
is ever sent. Use it to confirm the selectors still work after a Microsoft
UI change, or on a new machine before a shift.

Verified live on 2026-08-04 (outlook.live.com): all ten checks passed for
both `soc610529` (lowercase, no space) and `SOC 610454` (spaced). Note
`SOC610454` returned **no** results while `SOC 610454` matched — proof the
fallback query chain is needed.

A full GUI run on 2026-08-04 completed a real case end to end: SOC610454
was found, replied to, sent by the analyst, auto-detected and verified —
shown as **Sent ✓** in the tracker. Cases with no matching mail correctly
came back NOT FOUND.

**Still unproven:** only the ⋯-menu fallback on a live narrow window (the
maximised window always shows the toolbar button, which is the point).

### Testing tip

Only put case numbers in the sheet that actually have emails, or you'll
watch a lot of correct-but-slow NOT FOUND results — each one tries five
searches before giving up. `test_cases_real.xlsx` (in Downloads) holds the
two real test cases plus one deliberate miss.

## Window / monitors

The browser is moved to the **biggest screen** and maximised automatically
(a narrow window makes Outlook hide Reply all behind the ⋯ menu). Pick a
specific screen with the **Show browser on** dropdown, or
`--monitor 2` / `--monitor primary` / `--monitor current` on the command
line. `python soc_followup.py --csv x.xlsx --list-monitors` lists them.

## Auto-send (optional, off by default)

By default every reply waits for the analyst. Ticking **Auto-send** lets the
tool send without review — but only when **both** conditions hold:

- the **latest** message in the thread is from the sender you configure
  (email address *or* display name), and
- the thread contains your phrase (default `follow up`)

Anything else falls back to the normal manual flow, including threads it
can't read. A confirmation box must also be ticked — the reply goes out with
your Outlook signature as it is currently set.

Two details that matter, both learned from live testing:

- the sender is taken from the newest message's **From** line only, so a
  name appearing in `To:` — or the expected sender appearing further down a
  quoted thread — never counts
- matching ignores spacing, because Outlook wraps long addresses mid-word
  (`soc alerts@example.com`) and mixes icon characters into the same text

The configured sender lives in `soc_settings.json` beside the tool, which is
never committed or packaged. Every decision is logged with its reason, and
auto-sent replies are still verified in Sent Items.

CLI equivalent:

```
python soc_followup.py --csv cases.xlsx --auto-send \
    --auto-sender alerts@example.com --auto-keyword "follow up"
```

## Logs and diagnostics

Every run writes `logs\followup_<timestamp>.log` containing the environment
(Python, Selenium, Chrome and driver versions), each case, every search
query and what it matched, the row it opened, Sent Items checks, and full
tracebacks for anything that failed.

**Save diagnostics** in the GUI (or `--diagnostics` on the command line)
zips the logs, any failure screenshots and the run results into one file to
send on for troubleshooting. It contains case numbers and email subjects —
handle it like the XSOAR export. The signed-in browser profile is never
included.

## Troubleshooting

- **Stuck on "Waiting for the mailbox to load"** — sign in in the browser
  window; it waits up to 5 minutes.
- **Asks me to sign in again every launch** — that used to happen when a
  previous run left Chrome processes running: Chrome won't reuse a profile
  another process owns, so it silently starts a blank one. The tool now
  closes those leftovers before launching (only processes using its own
  `uc_profile` — your personal Chrome is never touched). If it ever
  recurs, close any stray automated Chrome windows and try again.
- **Signed out mid-run** — click **Yes** at "Stay signed in?". The
  undetected-Chrome engine plus one-page-per-run behaviour exists precisely
  to stop Microsoft's bot detection revoking the session. If it happens
  anyway, the tool pauses and waits for you to sign back in rather than
  failing the case.
- **It stores a logged-in session in `uc_profile\`** — treat that folder
  like a saved password. Delete it to force a fresh login.
- **"Reply all button not found"** — it first tries the toolbar button, then
  the **⋯ (More actions)** menu next to the message header, then *Other
  reply actions* inside it, so a narrow window that hides Reply all is
  handled. If it still fails, a screenshot lands in `screenshots\`;
  Microsoft occasionally changes the Outlook web UI and the selectors in
  `soc_followup.py` may need a small update.
- **Maximise the browser window** if the toolbar looks cramped — it makes
  Reply all a visible button instead of a menu entry.
- **Wrong column picked from the sheet** — pass `--column <header name>`.
- **Search finds the wrong email** — only rows containing the case number
  are considered, and it replies to the newest of those. The matched
  subject is shown before you send, so you can Skip if it looks wrong.
- **Says NOT FOUND but the mail is clearly there** — a screenshot of what
  the page looked like is saved in `screenshots\` as
  `case_<number>_notfound_*.png`, and the log lists every search it tried.
  Send those over and the query list can be extended.

## Notes for rolling this out at work

- The script never sends anything by itself — every email is reviewed and
  sent by the analyst, so the blast-radius of a bug is low.
- It stores a logged-in browser session in `uc_profile\`. Treat that folder
  like a saved password (it's on your profile, same as your normal browser).
  (`owa_profile\` is left over from the old Playwright engine — safe to
  delete.)
- It drives **Outlook on the web** only, which is what the SOC uses — no
  Outlook desktop app is needed or touched. Analysts can keep working in
  their own Outlook/browser while it runs; the tool uses its own window.
- Roadmap: the "analyst reviews and sends" step is a single isolated call in
  the code, so a future fully-automated mode (auto-send, no analyst in the
  loop) is a small, deliberate change — but keep the human in the loop until
  the search-and-match part has proven itself over real shifts.
