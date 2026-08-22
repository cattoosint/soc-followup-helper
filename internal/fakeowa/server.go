// Package fakeowa serves a stand-in Outlook on the web.
//
// It backs both the offline test suites and the tool's own self-test, so
// the whole pipeline can be proven on a locked-down desktop without a
// mailbox, a network, or any chance of sending real mail.
package fakeowa

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

// Page is a stand-in for Outlook on the web that uses the same ARIA roles,
// labels and layout the real selectors target. It exists so the whole browser
// layer can be exercised end to end without a mailbox, a network, or the risk
// of sending real mail.
//
// The JavaScript avoids template literals so it can live inside a Go raw
// string.
const Page = `<!doctype html>
<html>
<head><meta charset="utf-8"><title>Fake OWA</title></head>
<body>
  <nav>
    <div role="treeitem" title="Inbox" aria-selected="true"
         onclick="showFolder('Inbox')">Inbox</div>
    <div role="treeitem" title="Sent Items" aria-selected="false"
         onclick="showFolder('Sent Items')">Sent Items</div>
  </nav>

  <button aria-label="New mail" onclick="newMail()">New mail</button>

  <input id="topSearchInput" role="searchbox" aria-label="Search"
         placeholder="Search" onkeydown="if(event.key==='Enter'){runSearch();}">

  <div id="suggestions" role="listbox" aria-label="Suggestions" style="display:none">
    <div role="option" id="suggest-1">a suggestion, not a mail</div>
  </div>

  <div id="list" aria-label="Message list" role="listbox"></div>

  <div id="pane" style="display:none">
    <button aria-label="Reply all" onclick="replyAll()">Reply all</button>
    <button aria-label="More options" onclick="void 0">...</button>
    <div id="reading" aria-label="Reading pane"></div>
  </div>

  <div id="compose" style="display:none">
    <div role="textbox" aria-label="To" contenteditable="true" id="to"></div>
    <button aria-label="Cc" onclick="showCc()">Cc</button>
    <div role="textbox" aria-label="Cc" contenteditable="true" id="cc"
         style="display:none"></div>
    <input aria-label="Subject" id="subject">
    <div role="textbox" aria-label="Message body"></div>
    <button aria-label="Send" onclick="doSend()">Send</button>
  </div>

  <div id="empty" style="display:none">We couldn't find anything.</div>

<script>
// Each mail: the row label, the messages in the thread, and the body.
var MAILS = [
  {
    id: "m1",
    label: "Jordan Lee  SOC610529 Suspicious login  Wed 8/5/2026 1:10 PM",
    subject: "SOC610529 Suspicious login",
    messages: [
      "SOC Alerts <alerts@example.com> To:Analyst Mon 8/3/2026 9:00 AM",
      "Jordan Lee <jordan@example.com> To:SOC Team Wed 8/5/2026 1:10 PM"
    ],
    body: "SOC610529 Suspicious login. Please follow up on this case."
  },
  {
    id: "m2",
    label: "SOC Alerts  SOC1610529 A different case  Tue 8/4/2026 4:00 PM",
    subject: "SOC1610529 A different case",
    messages: ["SOC Alerts <alerts@example.com> To:Analyst Tue 8/4/2026 4:00 PM"],
    body: "SOC1610529 unrelated."
  },
  {
    id: "m3",
    label: "Jordan Lee  SOC700001 Older thread  Mon 8/3/2026 8:00 AM",
    subject: "SOC700001 Older thread",
    messages: ["Jordan Lee <jordan@example.com> To:SOC Team Mon 8/3/2026 8:00 AM"],
    body: "SOC700001 please follow up."
  },
  {
    id: "m4",
    label: "SOC Alerts  SOC700001 Newer thread  Fri 8/7/2026 6:30 PM",
    subject: "SOC700001 Newer thread",
    messages: [
      "SOC Alerts <alerts@example.com> To:Analyst Fri 8/7/2026 6:30 PM " +
      "FYI - From: Jordan Lee <jordan@example.com> Sent: Mon 8/3/2026 " +
      "To: SOC Team Subject: SOC700001"
    ],
    body: "SOC700001 quoted thread, please follow up."
  }
];

var sentItems = [];
var current = null;
var folder = "Inbox";

function esc(s) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/"/g, "&quot;");
}

function renderRows(mails) {
  var html = "";
  for (var i = 0; i < mails.length; i++) {
    var m = mails[i];
    html += '<div role="option" data-convid="' + m.id + '" aria-label="' +
            esc(m.label) + '" onclick="openMail(' + "'" + m.id + "'" + ')">' +
            esc(m.label) + '</div>';
  }
  document.getElementById("list").innerHTML = html;
  document.getElementById("empty").style.display = mails.length ? "none" : "block";
}

function showFolder(name) {
  folder = name;
  // Real OWA marks the open folder with aria-selected, and the tool relies on
  // it: a folder click that was swallowed used to leave the mailbox on the
  // inbox while reporting success.
  var items = document.querySelectorAll("[role=treeitem]");
  for (var i = 0; i < items.length; i++) {
    items[i].setAttribute("aria-selected",
      items[i].getAttribute("title") === name ? "true" : "false");
  }
  document.getElementById("pane").style.display = "none";
  if (name === "Sent Items") { renderRows(sentItems); } else { renderRows([]); }
}

function runSearch() {
  var q = document.getElementById("topSearchInput").value;
  var digits = (q.match(/[0-9]{5,8}/) || [""])[0];
  document.getElementById("suggestions").style.display = "none";
  if (!digits) { renderRows([]); return; }
  var hits = [];
  for (var i = 0; i < MAILS.length; i++) {
    if (MAILS[i].label.indexOf(digits) >= 0) { hits.push(MAILS[i]); }
  }
  renderRows(hits);
}

function byId(id) {
  for (var i = 0; i < MAILS.length; i++) {
    if (MAILS[i].id === id) { return MAILS[i]; }
  }
  return null;
}

function openMail(id) {
  current = byId(id);
  if (!current) { return; }
  var html = "";
  for (var i = 0; i < current.messages.length; i++) {
    html += '<div role="listitem">' + esc(current.messages[i]) + '</div>';
  }
  html += "<p>" + esc(current.body) + "</p>";
  document.getElementById("reading").innerHTML = html;
  document.getElementById("pane").style.display = "block";
  document.getElementById("compose").style.display = "none";
}

// newMail opens an empty compose form, as the real New mail button does.
function newMail() {
  current = null;
  document.getElementById("to").innerText = "";
  document.getElementById("cc").innerText = "";
  document.getElementById("cc").style.display = "none";
  document.getElementById("subject").value = "";
  document.getElementById("compose").style.display = "block";
}

// Cc is hidden until asked for, exactly as OWA hides it.
function showCc() {
  document.getElementById("cc").style.display = "block";
}

function replyAll() {
  if (!current) { return; }
  document.getElementById("subject").value = "RE: " + current.subject;
  document.getElementById("compose").style.display = "block";
}

function nowLabel() {
  var d = new Date();
  var h = d.getHours();
  var m = d.getMinutes();
  var ap = h >= 12 ? "PM" : "AM";
  h = h % 12;
  if (h === 0) { h = 12; }
  return h + ":" + (m < 10 ? "0" + m : m) + " " + ap;
}

function doSend() {
  var subject = document.getElementById("subject").value;
  // stamped with the current time, as Outlook stamps a just-sent reply -
  // the tool ignores replies older than the send it is confirming
  sentItems.push({
    id: "s" + (sentItems.length + 1),
    label: "To: SOC Team  " + subject + "  " + nowLabel(),
    subject: subject, messages: [], body: ""
  });
  document.getElementById("compose").style.display = "none";
}

// The suggestion popup appears while typing, exactly as the real one does.
document.getElementById("topSearchInput").addEventListener("input", function () {
  document.getElementById("suggestions").style.display =
    this.value.length > 2 ? "block" : "none";
});
</script>
</body>
</html>`

// Start serves the stand-in mailbox and returns its URL.
func Start() *httptest.Server { return StartWithSent(nil) }

// StartWithSent serves the mailbox with replies ALREADY in Sent Items.
//
// This is how a mailbox really looks on the second night: last night's reply
// for the same case is still sitting there. A tool that treats any matching
// row as proof will report tonight's un-sent case as replied, so the tests
// need to be able to build that state.
//
// Each label is one Sent Items row, exactly as Outlook would show it, e.g.
// "To: SOC Team  RE: SOC700001 Suspicious login  Mon 8/3/2026 9:00 AM".
// InboxMail is one mail to seed the stand-in inbox with.
type InboxMail struct {
	Case    string // e.g. "700123"
	Subject string
	Sender  string // display name and address, as the header line shows it
	Stamp   string // as Outlook renders it, e.g. "Wed 8/5/2026 1:10 PM"
	Body    string
}

// StartWithInbox serves a mailbox holding the given mail.
//
// A shift is 30-50 cases, and running that many through the tool exercises
// things three mails never will: the result list changing under it, one case's
// mail lingering while the next is searched, and every status appearing in one
// review sheet. Keeping the fake at three mails meant none of that was tested.
func StartWithInbox(mails []InboxMail, sentLabels []string) *httptest.Server {
	if len(mails) == 0 {
		return StartWithSent(sentLabels)
	}
	rows := make([]string, 0, len(mails))
	for i, m := range mails {
		label := fmt.Sprintf("%s  SOC%s %s  %s", m.Sender, m.Case, m.Subject, m.Stamp)
		header := fmt.Sprintf("%s To:Analyst %s", m.Sender, m.Stamp)
		rows = append(rows, fmt.Sprintf(
			`{id:"seed%d",label:%s,subject:%s,messages:[%s],body:%s}`,
			i, jsQuote(label), jsQuote("SOC"+m.Case+" "+m.Subject),
			jsQuote(header), jsQuote(m.Body)))
	}
	seed := "<script>MAILS.push(" + strings.Join(rows, ",") + ");</script>"
	return serve(seed + sentSeed(sentLabels))
}

func StartWithSent(sentLabels []string) *httptest.Server {
	return serve(sentSeed(sentLabels))
}

func sentSeed(sentLabels []string) string {
	if len(sentLabels) == 0 {
		return ""
	}
	rows := make([]string, 0, len(sentLabels))
	for i, label := range sentLabels {
		rows = append(rows, fmt.Sprintf(
			`{id:"sent%d",label:%s,subject:%s,messages:[],body:""}`,
			i, jsQuote(label), jsQuote(label)))
	}
	return "<script>sentItems.push(" + strings.Join(rows, ",") + ");</script>"
}

func serve(seed string) *httptest.Server {
	body := []byte(strings.Replace(Page, "</body>", seed+"</body>", 1))
	return httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(body)
		}))
}

// jsQuote renders a Go string as a JavaScript string literal.
func jsQuote(s string) string {
	out, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(out)
}
