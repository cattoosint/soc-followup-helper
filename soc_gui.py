#!/usr/bin/env python3
"""GUI front-end for the SOC nightshift follow-up helper.

Run:  python soc_gui.py   (or double-click run_followup.bat)

Pick the filtered XSOAR export (.xlsx keeps your Excel filter - hidden rows
are skipped), hit Start, and watch each case move through the tracker table:
Pending -> Searching -> Awaiting send -> Sent / NOT FOUND / Skipped / Error.
The analyst reviews and sends every email; the GUI only does the clicking
and the bookkeeping.
"""

import os
import queue
import sys
import threading
from datetime import datetime
import tkinter as tk
from tkinter import filedialog, messagebox, ttk
from types import SimpleNamespace

import soc_followup as core

URL_PRESETS = {
    "Work Outlook (outlook.cloud.microsoft)": "https://outlook.cloud.microsoft/mail/",
    "Work Outlook (outlook.office.com)": "https://outlook.office.com/mail/",
    "Personal test (outlook.live.com)": "https://outlook.live.com/mail/",
}

STATUS_TEXT = {
    "PENDING": "Pending",
    "WORKING": "Searching…",
    "REVIEW": "Awaiting send — review the draft",
    "SENT": "Sent ✓",
    "SKIPPED": "Skipped",
    "NOT_FOUND": "NOT FOUND ⚠",
    "ERROR": "Error",
    "QUIT": "Stopped",
}
FINAL_STATUSES = ("SENT", "SKIPPED", "NOT_FOUND", "ERROR", "QUIT")

BUTTONS = [
    ("", "Sent ✓  next case"),
    ("s", "Skip"),
    ("r", "Retry"),
    ("q", "Stop"),
]


class GuiUI:
    """core.run_followups talks to the window through these calls."""

    def __init__(self, app):
        self.app = app

    def log(self, msg):
        core.log.info("[ui] %s", str(msg).strip())
        self.app.events.put(("log", msg))

    def case_update(self, num, status, detail=""):
        self.app.events.put(("case", num, status, detail))

    def ask(self, prompt, choices, kind=None):
        self._drain_answers()
        self.app.events.put(("ask", prompt, choices, kind))
        return self.app.answer_q.get()  # blocks the worker thread only

    def ask_or_watch(self, prompt, choices, watch_fn, poll_s=0.7, kind=None):
        """Show the buttons, but also auto-continue once watch_fn() is True
        (the reply draft was sent in Outlook)."""
        self._drain_answers()
        self.app.events.put(("ask", prompt, choices, kind))
        while True:
            try:
                return self.app.answer_q.get(timeout=poll_s)
            except queue.Empty:
                pass
            try:
                if watch_fn():
                    self.app.events.put(("clear_ask",))
                    return "auto"
            except Exception:
                pass

    def _drain_answers(self):
        while True:
            try:
                self.app.answer_q.get_nowait()
            except queue.Empty:
                return


class App:
    def __init__(self, root):
        self.root = root
        root.title("SOC Nightshift Follow-up")
        root.geometry("920x680")
        self.events = queue.Queue()
        self.answer_q = queue.Queue()
        self.worker = None
        self.column = None
        self.cases = []

        pad = {"padx": 8, "pady": 4}

        # --- file picker -----------------------------------------------------
        frm = ttk.Frame(root)
        frm.pack(fill="x", **pad)
        ttk.Label(frm, text="Case sheet (.xlsx keeps your filter, .csv doesn't):").grid(
            row=0, column=0, sticky="w")
        self.file_var = tk.StringVar()
        entry = ttk.Entry(frm, textvariable=self.file_var, width=70)
        entry.grid(row=1, column=0, sticky="we", padx=(0, 6))
        entry.bind("<Return>", lambda _e: self.load_file())  # paste a path + Enter
        btns_file = ttk.Frame(frm)
        btns_file.grid(row=1, column=1)
        ttk.Button(btns_file, text="Browse...", command=self.browse).pack(side="left")
        ttk.Button(btns_file, text="Reload", command=self.load_file).pack(
            side="left", padx=(4, 0))
        ttk.Button(btns_file, text="Check export", command=self.check_export).pack(
            side="left", padx=(4, 0))
        self.file_info = ttk.Label(frm, text="", foreground="#0066cc")
        self.file_info.grid(row=2, column=0, columnspan=2, sticky="w")
        frm.columnconfigure(0, weight=1)

        # --- options ---------------------------------------------------------
        opts = ttk.Frame(root)
        opts.pack(fill="x", **pad)
        ttk.Label(opts, text="Mailbox:").grid(row=0, column=0, sticky="w")
        self.url_var = tk.StringVar(value=list(URL_PRESETS)[0])
        self.url_box = ttk.Combobox(opts, textvariable=self.url_var, width=40,
                                    values=list(URL_PRESETS))
        self.url_box.grid(row=0, column=1, columnspan=3, sticky="w", padx=(4, 16))

        ttk.Label(opts, text="Limit (0=all):").grid(row=1, column=0, sticky="w", pady=(4, 0))
        self.limit_var = tk.StringVar(value="0")
        ttk.Spinbox(opts, from_=0, to=999, textvariable=self.limit_var,
                    width=5).grid(row=1, column=1, sticky="w", padx=(4, 16), pady=(4, 0))
        ttk.Label(opts, text="Wait after send (s):").grid(row=1, column=2, sticky="w",
                                                          pady=(4, 0))
        self.delay_var = tk.StringVar(value="5")
        ttk.Spinbox(opts, from_=0, to=120, textvariable=self.delay_var,
                    width=5).grid(row=1, column=3, sticky="w", padx=(4, 16), pady=(4, 0))
        self.pause_var = tk.BooleanVar(value=True)
        ttk.Checkbutton(opts, text="Pause on not-found",
                        variable=self.pause_var).grid(row=1, column=4, sticky="w",
                                                      pady=(4, 0))
        self.stealth_var = tk.BooleanVar(value=False)
        ttk.Checkbutton(opts, text="Undetected browser mode (only if Outlook "
                                   "keeps signing you out)",
                        variable=self.stealth_var).grid(row=3, column=0,
                                                        columnspan=5, sticky="w",
                                                        pady=(4, 0))

        ttk.Label(opts, text="Show browser on:").grid(row=2, column=0, sticky="w",
                                                      pady=(4, 0))
        mons = core.describe_monitors()
        self.monitor_choices = ["Biggest screen (auto)"] + mons
        self.monitor_var = tk.StringVar(value=self.monitor_choices[0])
        ttk.Combobox(opts, textvariable=self.monitor_var, width=30, state="readonly",
                     values=self.monitor_choices).grid(row=2, column=1, columnspan=2,
                                                       sticky="w", padx=(4, 16),
                                                       pady=(4, 0))
        ttk.Label(opts, text=f"({len(mons)} screen(s) detected — the window is "
                             "maximised automatically)",
                  foreground="#666666").grid(row=2, column=3, columnspan=3,
                                             sticky="w", pady=(4, 0))

        # --- start + progress ------------------------------------------------
        bar = ttk.Frame(root)
        bar.pack(fill="x", **pad)
        self.start_btn = ttk.Button(bar, text="▶  Start follow-ups", command=self.start)
        self.start_btn.pack(side="left")
        self.signin_btn = ttk.Button(bar, text="Sign in to Outlook",
                                     command=self.sign_in)
        self.signin_btn.pack(side="left", padx=(8, 0))
        self.progress_lbl = ttk.Label(bar, text="", font=("Segoe UI", 10, "bold"))
        self.progress_lbl.pack(side="left", padx=16)
        ttk.Button(bar, text="Open output folder",
                   command=lambda: os.startfile(core.SCRIPT_DIR)).pack(side="right")
        ttk.Button(bar, text="Save diagnostics",
                   command=self.save_diagnostics).pack(side="right", padx=(0, 6))
        self.flagged_btn = ttk.Button(bar, text="⬇  Review sheet",
                                      state="disabled", command=self.open_flagged)
        self.flagged_btn.pack(side="right", padx=(0, 6))
        self.last_flagged = None

        # --- case tracker table ---------------------------------------------
        table_frame = ttk.Frame(root)
        table_frame.pack(fill="both", expand=True, padx=8)
        cols = ("case", "status", "detail")
        self.tree = ttk.Treeview(table_frame, columns=cols, show="headings",
                                 selectmode="browse")
        self.tree.heading("case", text="Case")
        self.tree.heading("status", text="Status")
        self.tree.heading("detail", text="Matched email / notes")
        self.tree.column("case", width=110, anchor="w", stretch=False)
        self.tree.column("status", width=220, anchor="w", stretch=False)
        self.tree.column("detail", width=480, anchor="w")
        vsb = ttk.Scrollbar(table_frame, orient="vertical", command=self.tree.yview)
        self.tree.configure(yscrollcommand=vsb.set)
        self.tree.pack(side="left", fill="both", expand=True)
        vsb.pack(side="right", fill="y")
        self.tree.tag_configure("PENDING", foreground="#888888")
        self.tree.tag_configure("WORKING", foreground="#0066cc")
        self.tree.tag_configure("REVIEW", foreground="#0066cc",
                                font=("Segoe UI", 9, "bold"))
        self.tree.tag_configure("SENT", foreground="#1a7f37")
        self.tree.tag_configure("SKIPPED", foreground="#888888")
        self.tree.tag_configure("NOT_FOUND", foreground="#cc0000",
                                font=("Segoe UI", 9, "bold"))
        self.tree.tag_configure("ERROR", foreground="#b35900")
        self.tree.tag_configure("QUIT", foreground="#888888")

        # --- action prompt ---------------------------------------------------
        self.prompt_lbl = ttk.Label(root, text="", font=("Segoe UI", 10, "bold"))
        self.prompt_lbl.pack(fill="x", padx=8, pady=(6, 0))
        btns = ttk.Frame(root)
        btns.pack(fill="x", **pad)
        self.buttons = {}
        for key, label in BUTTONS:
            b = ttk.Button(btns, text=label, state="disabled",
                           command=lambda k=key: self.answer(k))
            b.pack(side="left", padx=4)
            self.buttons[key] = b

        # --- log -------------------------------------------------------------
        self.log_txt = tk.Text(root, height=7, state="disabled",
                               font=("Consolas", 9), wrap="word")
        self.log_txt.pack(fill="x", padx=8, pady=(0, 8))

        root.after(100, self.poll)

    # ------------------------------------------------------------- callbacks

    def browse(self):
        path = filedialog.askopenfilename(
            title="Pick the filtered XSOAR export",
            filetypes=[("Excel and CSV", ("*.xlsx", "*.xlsm", "*.csv")),
                       ("Excel workbook", ("*.xlsx", "*.xlsm")),
                       ("CSV file", ("*.csv",)),
                       ("All files", ("*.*",))])
        if path:
            self.file_var.set(path)
            self.load_file()

    def save_diagnostics(self):
        """Bundle logs + screenshots + results so they can be sent on for
        troubleshooting (no browser session, no mailbox access)."""
        try:
            path, added = core.collect_diagnostics()
        except Exception as e:
            messagebox.showerror("Diagnostics", f"Couldn't build it: {e}")
            return
        self.log(f"Diagnostics saved: {path.name}")
        messagebox.showinfo(
            "Diagnostics saved",
            f"{path.name}\n\n{len(added)} file(s): logs, any failure "
            "screenshots and the run results.\n\nIt contains case numbers and "
            "email subjects — treat it like the XSOAR export. Your signed-in "
            "browser session is NOT included.")
        os.startfile(core.SCRIPT_DIR)

    def check_export(self):
        """Dry-run report on the loaded sheet - especially useful the first
        time a new XSOAR export format is used."""
        path = self.file_var.get().strip().strip('"').strip("'")
        if not path:
            messagebox.showinfo("Check export", "Pick a sheet first.")
            return
        try:
            report = core.preflight(path)
        except Exception as e:
            messagebox.showerror("Check export", str(e))
            return
        text = core.format_preflight(report)
        if report["unreadable"] or report.get("header_is_data"):
            messagebox.showwarning("Check export — some rows will be ignored", text)
        else:
            messagebox.showinfo("Check export", text)

    def load_file(self):
        # "Copy as path" wraps the path in quotes; drag-and-drop can too
        path = self.file_var.get().strip().strip('"').strip("'")
        if not path:
            return
        self.file_var.set(path)
        stats = {}
        try:
            self.column, self.cases = core.extract_cases(path, stats=stats)
        except Exception as e:
            self.column, self.cases = None, []
            msg = str(e)
            if isinstance(e, PermissionError) or "Permission" in msg:
                msg = "file is locked - close it in Excel first, then Reload"
            elif isinstance(e, FileNotFoundError):
                msg = "file not found - check the path"
            self.file_info.config(text=f"Problem: {msg}", foreground="#cc0000")
            self.fill_table([])
            return
        hidden = stats.get("hidden_skipped", 0)
        note = f", {hidden} filtered-out row(s) skipped" if hidden else ""
        bad = len(stats.get("unreadable", []))
        if bad:
            note += f" — ⚠ {bad} row(s) have no readable case number " \
                    "(click 'Check export')"
        self.file_info.config(
            text=f"Column '{self.column}' → {len(self.cases)} case(s){note}",
            foreground="#b35900" if bad else "#0066cc")
        self.fill_table(self.cases)

    def fill_table(self, cases):
        self.tree.delete(*self.tree.get_children())
        for num in cases:
            self.tree.insert("", "end", iid=num,
                             values=(f"SOC{num}", STATUS_TEXT["PENDING"], ""),
                             tags=("PENDING",))
        self.progress_lbl.config(text=f"0 / {len(cases)} done" if cases else "")
        # a review sheet can be pulled at any point, including mid-run
        self.flagged_btn.config(state="normal" if cases else "disabled")

    def _make_opts(self, url):
        try:
            send_delay = float(self.delay_var.get() or 0)
        except ValueError:
            send_delay = 5.0
        choice = self.monitor_var.get()
        monitor = "largest"
        if choice.startswith("Monitor "):
            monitor = choice.split()[1].rstrip(":")
        return SimpleNamespace(
            url=url,
            profile_dir=str(core.SCRIPT_DIR / "uc_profile"),
            settle=3.0,
            send_delay=send_delay,
            monitor=monitor,
            engine="stealth" if self.stealth_var.get() else "standard",
            no_pause=not self.pause_var.get(),
        )

    def sign_in(self):
        url = URL_PRESETS.get(self.url_var.get(), self.url_var.get().strip())
        if not url.startswith("http"):
            messagebox.showwarning("Outlook URL", "Pick or paste a valid Outlook URL.")
            return
        opts = self._make_opts(url)
        self.signin_btn.config(state="disabled")
        self.start_btn.config(state="disabled")
        self.log("Opening Outlook so you can sign in once...")
        ui = GuiUI(self)

        def work():
            try:
                if core.sign_in_only(opts, ui):
                    ui.log("Session saved - later runs go straight to the mailbox.")
            except Exception as e:
                ui.log(f"ERROR: {e}")
            finally:
                self.events.put(("done",))

        threading.Thread(target=work, daemon=True).start()

    def start(self):
        if not self.cases:
            messagebox.showwarning("No cases", "Pick a sheet with case numbers first.")
            return
        cases = list(self.cases)
        try:
            limit = int(self.limit_var.get() or 0)
        except ValueError:
            limit = 0
        if limit > 0:
            cases = cases[:limit]
        url = URL_PRESETS.get(self.url_var.get(), self.url_var.get().strip())
        if not url.startswith("http"):
            messagebox.showwarning("Outlook URL", "Pick or paste a valid Outlook URL.")
            return
        self.fill_table(cases)
        opts = self._make_opts(url)
        self.start_btn.config(state="disabled")
        self.signin_btn.config(state="disabled")
        self.log(f"Starting: {len(cases)} case(s)")
        ui = GuiUI(self)
        src, col = self.file_var.get(), self.column

        def work():
            try:
                summary = core.run_followups(src, col, cases, opts, ui)
                self.events.put(("summary", summary))
            except Exception as e:
                core.log_exception("run failed", e)
                ui.log(f"ERROR: {e}")
                ui.log("Click 'Save diagnostics' and send the zip on.")
            finally:
                self.events.put(("done",))

        self.worker = threading.Thread(target=work, daemon=True)
        self.worker.start()

    def open_flagged(self):
        """Write a colour-coded copy of the sheet from wherever the run has got
        to, and open it. Works mid-run as well as at the end."""
        src, col = self.file_var.get(), self.column
        if not src or not col:
            return
        status_by_case = {}
        for iid in self.tree.get_children():
            tags = self.tree.item(iid, "tags")
            status = tags[0] if tags else "PENDING"
            if status in ("WORKING", "REVIEW"):
                status = "PENDING"          # started but not finished
            status_by_case[str(iid)] = status
        stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
        out = core.SCRIPT_DIR / f"followup_review_{stamp}.xlsx"
        try:
            core.write_status_xlsx(src, col, status_by_case, out)
        except Exception as e:
            messagebox.showerror("Review sheet", f"Couldn't write it: {e}")
            return
        self.last_flagged = str(out)
        self.log(f"Review sheet saved: {out.name}")
        os.startfile(str(out))

    def show_summary(self, summary):
        """End-of-run report: what got replied to, what still needs a human."""
        results = summary.get("results", [])
        sent = sum(1 for r in results if r["status"] == "SENT")
        flagged = summary.get("flagged", [])
        if summary.get("xlsx"):
            self.last_flagged = str(summary["xlsx"])
        if results:
            self.flagged_btn.config(state="normal")

        msg = f"{sent} replied, {len(flagged)} still need follow-up, " \
              f"{len(results)} processed."
        self.prompt_lbl.config(text=msg)
        detail = [msg, "", f"Results log: {os.path.basename(summary['out_csv'])}"]
        if self.last_flagged:
            detail += [f"Review sheet: {os.path.basename(self.last_flagged)}",
                       "   green = replied, red = needs follow-up, "
                       "yellow = not done"]
        if flagged:
            detail += ["", "NEEDS MANUAL FOLLOW-UP:"]
            detail += [f"   {r['case']}  ({r['status']})" for r in results
                       if r["status"] in ("NOT_FOUND", "ERROR")][:15]
            detail += ["", "Click '⬇ Review sheet' any time to reopen it."]
            messagebox.showwarning("Follow-up still needed", "\n".join(detail))
        else:
            messagebox.showinfo("Run finished", "\n".join(detail))

    def answer(self, key):
        for b in self.buttons.values():
            b.config(state="disabled")
        self.prompt_lbl.config(text="")
        self.answer_q.put(key)

    # ---------------------------------------------------------------- events

    def log(self, msg):
        self.log_txt.config(state="normal")
        self.log_txt.insert("end", msg + "\n")
        self.log_txt.see("end")
        self.log_txt.config(state="disabled")

    def update_case(self, num, status, detail):
        if num not in self.tree.get_children():
            return
        text = STATUS_TEXT.get(status, status)
        self.tree.item(num, values=(f"SOC{num}", text, detail), tags=(status,))
        self.tree.see(num)
        done = sum(
            1 for iid in self.tree.get_children()
            if self.tree.item(iid, "tags") and
            self.tree.item(iid, "tags")[0] in FINAL_STATUSES
        )
        total = len(self.tree.get_children())
        self.progress_lbl.config(text=f"{done} / {total} done")

    def show_ask(self, prompt, choices, kind=None):
        texts = {"": "Sent ✓  next case", "s": "Skip", "r": "Retry", "q": "Stop"}
        if kind == "verify":
            msg = ("Draft closed but nothing matching turned up in Sent Items "
                   "— was it actually sent?")
            texts[""] = "Yes — it sent"
            texts["s"] = "No — mark skipped"
            texts["r"] = "Retry case"
        elif kind == "notfound" or "s" not in choices:
            msg = "Case NOT FOUND — flagged for manual follow-up. Continue?"
            texts[""] = "Continue"
        else:
            msg = ("Review the reply-all draft in Outlook and hit Send — "
                   "it will continue automatically (buttons work too).")
        self.prompt_lbl.config(text=msg)
        for key, b in self.buttons.items():
            b.config(text=texts[key],
                     state="normal" if key in choices else "disabled")

    def poll(self):
        try:
            while True:
                ev = self.events.get_nowait()
                kind = ev[0]
                if kind == "log":
                    self.log(ev[1])
                elif kind == "case":
                    self.update_case(ev[1], ev[2], ev[3])
                elif kind == "ask":
                    self.show_ask(ev[1], ev[2], ev[3] if len(ev) > 3 else None)
                elif kind == "clear_ask":
                    for b in self.buttons.values():
                        b.config(state="disabled")
                    self.prompt_lbl.config(text="")
                elif kind == "summary":
                    self.show_summary(ev[1])
                elif kind == "done":
                    self.start_btn.config(state="normal")
                    self.signin_btn.config(state="normal")
                    if not self.prompt_lbl.cget("text"):
                        self.prompt_lbl.config(text="Finished — results are "
                                                    "in the output folder.")
        except queue.Empty:
            pass
        self.root.after(100, self.poll)


def main():
    try:
        from ctypes import windll
        windll.shcore.SetProcessDpiAwareness(1)  # crisp text on HiDPI screens
    except Exception:
        pass
    log_path = core.setup_logging()
    root = tk.Tk()
    app = App(root)
    if log_path:
        app.log(f"Logging to logs\\{log_path.name}")
    if len(sys.argv) > 1:  # dragged onto run_followup.bat
        app.file_var.set(sys.argv[1])
        app.load_file()
    root.mainloop()


if __name__ == "__main__":
    main()
