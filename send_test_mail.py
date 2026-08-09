"""Create test emails for trying the tool out - TEST MAILBOXES ONLY.

Sends from whichever mailbox the tool is signed into. By default it mails
that same mailbox, so nothing leaves it and the reply-all path can be
exercised safely.

    python send_test_mail.py 612100 612101          to yourself
    python send_test_mail.py 612100 --to a@b.com    somewhere else
    python send_test_mail.py 612100 --no-followup   without the phrase

Each mail is subject "SOC <num> ..." with "Please follow up on this case."
in the body, which is what the auto-send rule looks for.
"""

import re
import sys
import time
from types import SimpleNamespace

import soc_followup as core
from selenium.webdriver.common.by import By

SUBJECT = "SOC {num} Suspicious login from unusual location"
BODY_FOLLOWUP = ("Hi team,\n\nPlease follow up on this case and confirm the "
                 "action taken.\n\nRegards,\nSOC Alerts")
BODY_PLAIN = ("Hi team,\n\nThis one is informational only, no action "
              "required.\n\nRegards,\nSOC Alerts")


class UI:
    def log(self, m):
        print("   ", m)


def signed_in_address(driver):
    """The mailbox we are signed into. Only the account button counts - a
    sender address from the message list would be someone else entirely."""
    for css in ("#O365_MainLink_Me", "[aria-label*='Account manager' i]",
                "button[aria-label*='Signed in as' i]"):
        for el in driver.find_elements(By.CSS_SELECTOR, css):
            try:
                label = el.get_attribute("aria-label") or ""
                match = re.search(r"[\w.+-]+@[\w.-]+", label)
                if match:
                    return match.group()
            except Exception:
                continue
    return ""


def compose(driver, to_addr, subject, body):
    new_mail = core._find_first(driver, [
        "button[aria-label='New mail']", "[aria-label='New mail']",
        "button[aria-label*='New mail' i]"]) or core._find_by_text(driver, "New mail")
    if not core._click_el(new_mail):
        raise RuntimeError("could not find the New mail button")
    time.sleep(2.5)

    # the recipient box is a contenteditable div, not an <input>
    to_box = core._find_first(driver, [
        "div[contenteditable='true'][aria-label='To']",
        "[contenteditable='true'][aria-label*='To' i]",
        "input[aria-label='To']"])
    if to_box is None:
        raise RuntimeError("could not find the To field")
    to_box.click()
    to_box.send_keys(to_addr)
    time.sleep(1.2)
    from selenium.webdriver.common.keys import Keys
    to_box.send_keys(Keys.ENTER)      # accept the recipient
    time.sleep(0.6)

    subj = core._find_first(driver, [
        "input[aria-label='Subject']", "input[placeholder='Add a subject']",
        "input[aria-label*='subject' i]"])
    if subj is None:
        raise RuntimeError("could not find the Subject field")
    subj.click()
    subj.send_keys(subject)
    time.sleep(0.4)

    editor = core._find_first(driver, [
        "[role='textbox'][aria-label='Message body']",
        "div[contenteditable='true'][aria-label='Message body']",
        "[aria-label*='message body' i]"])
    if editor is None:
        raise RuntimeError("could not find the message body")
    editor.click()
    editor.send_keys(body)
    time.sleep(0.4)

    if not core.click_send(driver):
        raise RuntimeError("could not find the Send button")
    time.sleep(2.5)


def main():
    argv = sys.argv[1:]
    nums = [a for a in argv if a.isdigit()]
    if not nums:
        sys.exit("usage: python send_test_mail.py <case number> [more...] "
                 "[--to addr] [--no-followup]")
    to_addr = argv[argv.index("--to") + 1] if "--to" in argv else None
    body = BODY_PLAIN if "--no-followup" in argv else BODY_FOLLOWUP
    url = argv[argv.index("--url") + 1] if "--url" in argv else \
        "https://outlook.live.com/mail/"

    core.setup_logging()
    opts = SimpleNamespace(profile_dir=str(core.SCRIPT_DIR / "uc_profile"),
                           monitor="largest", engine="standard", url=url)
    driver = core._launch_browser(opts, UI())
    try:
        driver.get(url)
        if core.wait_for_mailbox(driver, 180) is None:
            sys.exit("not signed in - run the tool once and sign in first")

        target = to_addr or signed_in_address(driver)
        if not target:
            sys.exit("could not work out this mailbox's address - pass --to")
        print(f"\nsending {len(nums)} test mail(s) to {target}")
        print(f"body contains 'follow up': {body is BODY_FOLLOWUP}\n")

        for num in nums:
            compose(driver, target, SUBJECT.format(num=num), body)
            print(f"   sent: SOC {num}")

        print("\nDone. Give Outlook a moment, then the cases are ready to try.")
    finally:
        try:
            driver.quit()
        except Exception:
            pass
        time.sleep(2)
        core._release_stale_profile(opts.profile_dir)


if __name__ == "__main__":
    main()
