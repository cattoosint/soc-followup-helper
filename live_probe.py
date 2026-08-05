#!/usr/bin/env python3
"""Live selector probe against the real Outlook web UI.

Exercises every page interaction the tool relies on - search, result rows,
opening a mail, Reply all (toolbar and the '...' menu), compose detection,
and the Sent Items check - reporting exactly which selector worked.

NOTHING IS EVER SENT: the reply draft is opened, inspected, then discarded.

    python live_probe.py 610529
    python live_probe.py 610529 --url https://outlook.office.com/mail/
"""

import sys
import time
from types import SimpleNamespace

import soc_followup as core
from selenium.webdriver.common.by import By


class UI:
    def log(self, msg):
        print("   ", msg)


def hdr(text):
    print("\n" + "=" * 66)
    print(text)
    print("=" * 66)


def dump_buttons(driver, limit=25):
    """Aria-labels of visible buttons - the raw material for selectors."""
    seen = []
    try:
        for el in driver.find_elements(By.CSS_SELECTOR, "button, [role='menuitem']"):
            try:
                if not el.is_displayed():
                    continue
                lbl = (el.get_attribute("aria-label") or el.text or "").strip()
                if lbl and lbl not in seen:
                    seen.append(lbl)
            except Exception:
                continue
    except Exception:
        pass
    print("    visible buttons/menu items:", ", ".join(seen[:limit]) or "(none)")


def main():
    num = sys.argv[1] if len(sys.argv) > 1 and sys.argv[1].isdigit() else "610529"
    url = "https://outlook.live.com/mail/"
    if "--url" in sys.argv:
        url = sys.argv[sys.argv.index("--url") + 1]

    ui = UI()
    engine = "stealth" if "--stealth" in sys.argv else "standard"
    opts = SimpleNamespace(profile_dir=str(core.SCRIPT_DIR / "uc_profile"),
                           monitor="largest", url=url, settle=3.0,
                           engine=engine)
    results = {}

    log_path = core.setup_logging()
    hdr(f"LIVE PROBE - case {num} @ {url}")
    if log_path:
        print(f"    logging to logs\\{log_path.name}")
    driver = core._launch_browser(opts, ui)
    core.log_browser(driver)
    try:
        driver.get(url)
        box = core.wait_for_mailbox(driver, 180)
        results["mailbox loads / signed in"] = box is not None
        if box is None:
            print("    NOT SIGNED IN - sign in, then re-run.")
            return 1
        print(f"    signed in, search box found: {box.get_attribute('id') or 'ok'}")

        # ---- 1. search -------------------------------------------------
        hdr("1. SEARCH")
        hit_query, rows = None, []
        for q in core.build_queries(num):
            found = core.run_search(driver, q, opts.settle, num=num)
            print(f"    {q!r:26} -> {len(found) if found else 0} match(es)")
            if found and not hit_query:
                hit_query, rows = q, found
                break
        results[f"search finds the mail"] = bool(rows)
        if not rows:
            print("    no query matched - dumping what the page shows:")
            for i, css in enumerate(core.MESSAGE_LIST_SELECTORS):
                try:
                    n = len([e for e in driver.find_elements(By.CSS_SELECTOR, css)
                             if e.is_displayed()])
                except Exception:
                    n = "err"
                print(f"      [{i}] {css[:50]:52} -> {n}")
            core.screenshot(driver, core.SCRIPT_DIR / "screenshots", "probe_nosearch")
            return 1

        # which selector produced the rows?
        for i, css in enumerate(core.MESSAGE_LIST_SELECTORS):
            try:
                n = len([e for e in driver.find_elements(By.CSS_SELECTOR, css)
                         if e.is_displayed()])
            except Exception:
                n = 0
            if n:
                print(f"    rows come from selector [{i}]: {css}")
                results["message list scoped (not the suggestion popup)"] = i < 5
                break

        # ---- 2. row labels + timestamps --------------------------------
        hdr("2. ROW LABELS / TIMESTAMP PARSING")
        parsed_ok = 0
        for el in rows[:5]:
            lbl = core.label_of(el)
            ts = core.parse_row_time(lbl)
            parsed_ok += 1 if ts else 0
            print(f"    {lbl[:70]:72} -> {ts}")
        results["timestamps parsed from real rows"] = parsed_ok > 0
        results["case number matched with digit boundaries"] = all(
            core.label_matches_case(core.label_of(e), num) for e in rows)

        # ---- 3. open the newest ----------------------------------------
        hdr("3. OPEN THE MAIL")
        chosen = core.sort_newest_first(rows)[0]
        print(f"    opening: {core.label_of(chosen)[:70]}")
        chosen.click()
        time.sleep(1.5)
        opened = core.wait_for_reading_pane(driver)
        results["mail opens (reading pane detected)"] = opened
        print(f"    reading pane detected: {opened}")
        if not opened:
            dump_buttons(driver)
            core.screenshot(driver, core.SCRIPT_DIR / "screenshots", "probe_noopen")
            return 1

        # ---- 4. Reply all ----------------------------------------------
        hdr("4. REPLY ALL")
        direct = core._find_first(driver, [
            "button[aria-label='Reply all']", "[aria-label='Reply all']",
            "button[title='Reply all']", "[aria-label*='Reply all']"])
        print(f"    toolbar Reply all button present: {direct is not None}")
        if direct is None:
            dump_buttons(driver, 18)      # diagnostics only when something's off
        clicked = core.click_reply_all(driver)
        results["Reply all clicked"] = clicked
        print(f"    click_reply_all() -> {clicked}"
              f" ({'direct button' if direct is not None else 'via ... menu'})")
        if not clicked:
            core.screenshot(driver, core.SCRIPT_DIR / "screenshots", "probe_noreplyall")
            return 1

        time.sleep(2)
        compose = core.compose_is_open(driver)
        results["compose/draft detected"] = compose
        print(f"    compose_is_open() -> {compose}")

        # ---- 5. send-watcher (without sending!) -------------------------
        hdr("5. SEND DETECTION (nothing is sent)")
        watch = core.make_sent_watcher(driver)
        first = watch()
        print(f"    watcher while draft is open -> {first} (must be False)")
        results["watcher stays quiet while the draft is open"] = (first is False)

        # ---- 6. Sent Items check ---------------------------------------
        hdr("6. SENT ITEMS CHECK")
        verdict = core.verify_sent_web(driver, num, ui)
        print(f"    verify_sent_web() -> {verdict}"
              "   (False/None expected - we never sent anything)")
        results["Sent Items folder reachable"] = verdict is not None
        print(f"    back in folder: ok")

        # ---- 7. discard the draft --------------------------------------
        hdr("7. CLEAN UP - DISCARD THE DRAFT")
        discard = core._find_first(driver, [
            "button[aria-label='Discard']", "[aria-label='Discard']",
            "button[title='Discard']"]) or core._find_by_text(driver, "Discard")
        if discard is not None:
            try:
                discard.click()
                time.sleep(1)
                confirm = core._find_by_text(driver, "Discard")
                if confirm is not None:
                    confirm.click()
                print("    draft discarded")
            except Exception as e:
                print(f"    couldn't discard ({e}); it may sit in Drafts")
        else:
            print("    no Discard button found; draft may remain in Drafts")
        return 0
    finally:
        hdr("RESULT")
        for label, ok in results.items():
            print(f"[{'OK ' if ok else 'FAIL'}] {label}")
        bad = [k for k, v in results.items() if not v]
        print("-" * 66)
        print("LIVE PROBE PASSED" if not bad else f"PROBLEMS: {len(bad)}")
        try:
            driver.quit()
        except Exception:
            pass
        time.sleep(2)
        core._release_stale_profile(opts.profile_dir)


if __name__ == "__main__":
    sys.exit(main())
