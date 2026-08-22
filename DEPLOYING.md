# Getting this running on a locked-down machine

A runbook for the situation this tool exists for: a SOC desktop where you
cannot install what you like and most transfer routes are blocked.

**The short version: get the Go toolchain, then build. There is nothing else to
obtain.** This tool has no dependencies - standard library only - so there is
no module proxy to arrange, no `vendor/` directory to move across, and nothing
to ask Artifactory for.

---

## Build

```
go build -ldflags="-s -w" -trimpath -o bin/socfollowup.exe ./cmd/socfollowup
socfollowup.exe --self-test
```

That is the whole procedure. It works with the network switched off:

```
set GOPROXY=off
set GOTOOLCHAIN=local
go build ./cmd/socfollowup
```

**Go 1.24 or newer.** `go.mod` says `go 1.24.0`; newer toolchains build it
fine. Set `GOTOOLCHAIN=local` so an older Go reports a version problem plainly
instead of trying to fetch a newer toolchain over a network that is blocked.

### The toolchain does not need installing

Check the Software Center first - Go is often published there, which is the
sanctioned route and the easiest ask. Failing that, Go ships as a plain zip:

- `go1.X.Y.windows-amd64.zip` from `go.dev/dl` (about 76 MB)
- Extract it anywhere you can write; your profile is fine
- `set PATH=C:\path\to\go\bin;%PATH%`

Nothing is installed, nothing touches the registry, and you can delete the
folder afterwards.

### Do not run `go mod tidy`

There is nothing to tidy, and it reaches for the network to prove it.
`go build` is the whole story.

---

## Prove it before trusting it

```
socfollowup.exe --self-test
```

This drives a stand-in mailbox served from inside the binary. Nothing leaves
the machine and no mail can be sent. It checks the three outcomes that matter:

```
[ok  ] auto-sends when the newest mail really is from the sender - SOC610529
[ok  ] refuses when the sender is only quoted                     - SOC700001
[ok  ] flags a case with no matching mail                         - SOC999999
[ok  ] wrote a colour-coded review sheet
```

If those pass, the machine is fine, and anything that goes wrong afterwards is
Outlook-side.

---

## Working out what is blocked

Run these in order and note the first that fails.

| Check | Command | If it fails |
|---|---|---|
| Go present | `go version` | Software Center, or the zip above |
| Go new enough (1.24+) | `go version` | Same - a newer zip |
| It builds | `go build ./cmd/socfollowup` | Read the error; there are no dependencies, so it is the toolchain |
| A browser to drive | `go run ./cmd/chromecheck` | Nothing will work until Chrome or Edge is reachable |
| Chrome can be driven | `go run ./cmd/chromecheck` | Automation is blocked; check EDR or policy |
| The whole tool works | `socfollowup.exe --self-test` | The failure names which guard broke |

---

## Fallback: the Python build

The Python version is the one with real mileage - a 50-case run against a live
mailbox and 72 automated checks. Reach for it if the Go toolchain is genuinely
unavailable, or if the people approving things want to *read* what they are
approving: a `.py` file is text a reviewer can follow, and an unsigned `.exe`
is not.

It needs Python 3.10+, the bundled `libs/` folder, and `chromedriver.exe`.

**There is no Python substitute for the Go compiler.** You cannot build Go
source with anything else. The fallback is not "build it another way", it is
"use the Python tool instead".

---

## Transfer routes, and what blocks each

Recorded because a lot of time went into finding out.

| Route | Status | Why |
|---|---|---|
| Source zip (~110 KB) | **Works** | Pure text, nothing on any blocklist, far below any size limit |
| Vendored source zip (~4 MB) | Refused | Too large once base64-encoded: `550 Message Size Violation` |
| Earlier vendored zip, with chromedp | Blocked | 10 `.js` files inside, and `.js` is blocked inside archives |
| Built `.exe` (~8 MB) | Blocked | `.exe` is blocked outright; unsigned makes it worse |
| Google Drive | Blocked | |
| Public GitHub download | Rejected by review | |
| PyPI / module proxies on the open internet | Blocked | |
| Artifactory / JFrog | The sanctioned route, no longer needed for this | |

Two properties made the bundle deliverable, and both are easy to lose:
**no third-party dependencies**, and **no file type a mail gateway treats as
executable**. Adding a library back can undo either without anyone noticing
until the next delivery fails.

**Do not try to work around a block.** Renaming an `.exe`, nesting zips or
splitting an archive all defeat a control that exists to stop exactly that, and
they turn a supportable tool into something nobody will sign off on. This
bundle gets through because it is genuinely harmless, not because it is
disguised.

---

## If files went missing in transit: the console-only build

A mail gateway that sanitises attachments will strip or rewrite any file
containing an HTML document. Two files here are exactly that:

- `internal/web/page.go` - this **is** the tracker page: HTML, CSS and
  JavaScript in a Go string;
- `internal/fakeowa/server.go` - the stand-in Outlook `--self-test` drives.

`main.go` imports both, so when they do not arrive nothing builds, and the
error names a missing package rather than a missing file. (`tools/gen_truth.py`
may also be converted; it regenerates the conformance fixture from the Python
original and has no role on this machine - ignore it.)

Build without them:

```
go build -tags cli -ldflags="-s -w" -trimpath -o bin\socfollowup.exe .\cmd\socfollowup
```

**What you lose:**

- **The tracker window.** The console runs the same engine: it asks its
  questions in the terminal and writes the same results CSV and colour-coded
  review sheet.
- **`--self-test`** - the one command that proves the tool works on a machine
  before it touches real mail. Use `--diagnose SOC<case>` instead. It needs a
  real mailbox, but opens no mail and sends nothing, and it reports whether the
  browser, the selectors and the date format work on that build of Outlook.
  Do not skip it: on a console-only build it is the only pre-flight you have.

**What you keep:** searching, opening the newest matching mail, Reply all, the
analyst's send, auto-send and every one of its guards, the Sent Items check,
the results CSV and the review sheet.

This is a smaller tool, not a smuggled one - a `cli` build genuinely contains
no HTML, and the two features that need it genuinely are not there. It is a
stopgap. A full copy through a sanctioned route is better, and the transfer
options are in the next section.

## Before the first run on a corporate mailbox

This section is about permission, not software. The rest of this document is
careful about moving code onto a managed machine; this is the part about
pointing that code at someone's mailbox.

**What this tool does on your behalf.** It signs in as you, and it clicks
**Reply all** - so a reply reaches everyone already on the thread, carrying
your signature, from your address. With auto-send on it does that without a
human reading the message first. Anyone receiving it sees mail from you, not
from a tool.

**Get this settled before the first run, not after:**

- **The mailbox owner.** If it is not your own mailbox - a shared SOC or team
  mailbox - ask whoever owns it. "I am going to automate follow-up replies
  from this mailbox" is the whole question.
- **Your security or IT team.** Browser automation against a corporate tenant
  may be against policy regardless of intent, and conditional-access or
  session policies can treat it as suspicious sign-in behaviour. Better to be
  told no than to be found out.
- **Say what it sends.** Not "it helps with follow-ups" - say that it replies
  to all recipients on the thread, and whether you intend to enable auto-send.

If any of that is refused, that is the answer. Do not work around it: the same
rule this document already applies to software transfer applies here.

## The first live run, in order

Do not skip a step because the one before it passed. Each proves something the
next one assumes.

1. **`socfollowup.exe --self-test`** - proves the machine: Chrome or Edge is
   driveable, the matching rules work, the review sheet writes. No mailbox
   involved, nothing sent. About fifteen seconds.

2. **`socfollowup.exe --diagnose SOC<a real case>`** - proves *this tenant*.
   Sign in when the browser opens. It searches for one case and reports which
   link in the chain works or breaks. **It opens no mail and sends nothing.**
   It writes `diagnose_SOC<case>_<timestamp>.txt` next to the executable,
   containing only the row labels Outlook itself shows - safe to send to
   whoever maintains this.

   This step matters more than it looks. Every defect found the first time
   this tool met a real mailbox was specific to that OWA build: a private-use
   icon glyph in the folder names, a search box that dropped typed text, a
   window too narrow to show Reply all. A corporate tenant is a different
   build again.

3. **`socfollowup.exe --csv <your export> --check`** - proves the sheet.
   Reads it and reports what a run would do. Touches nothing.

4. **A supervised run of one or two cases, auto-send OFF.** In the tracker set
   *Stop after N cases* to 1. Watch the whole thing: the search, the mail it
   opens, the draft. Send it yourself. Confirm the row goes green and that the
   green is deserved.

5. **A full shift, auto-send still OFF.** This is the normal way to use the
   tool, and for most people it is where it stops.

6. **Auto-send, only after all of the above**, only with the sender and phrase
   set deliberately, and **not unattended the first time**. Watch the first
   several cases go out.

**The signed-in session lives in `uc_profile/` beside the executable. Treat
that directory as credential material** - it is what keeps you signed in
between runs. Do not copy it to another machine, do not put it on a share, and
delete it when you are done with the machine.

## Once it builds

```
socfollowup.exe --self-test                  prove the machine works, touches nothing
socfollowup.exe                              the tracker page, in your browser
socfollowup.exe --csv sheet.xlsx --check     what would a run do?
```

Give it the **`.xlsx`** you filtered in Excel, not a CSV. Rows you filtered out
are skipped, but a filter only survives inside a workbook - a CSV has no hidden
rows, so everything you excluded would get followed up too.

Leave auto-send off for the first real shift.

See `CLAUDE.md` for the code itself: layout, invariants, and the things that
have already broken once.
