package web

// indexHTML is the tracker page. It is served from the binary, so there is no
// asset directory to ship and nothing is fetched from the internet.
//
// The JavaScript deliberately avoids template literals: this file is a Go raw
// string, and a stray backtick would end it.
const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>SOC follow-up</title>
<style>
  :root {
    --bg: #f6f7f9; --panel: #ffffff; --ink: #1c2024; --muted: #667085;
    --line: #e3e6ea; --accent: #2f5fd0;
    --green: #1f7a3d; --green-bg: #e8f5ec;
    --amber: #8a6100; --amber-bg: #fdf3dc;
    --red: #a11c2b;   --red-bg: #fbe9eb;
    --blue: #1f5f8a;  --blue-bg: #e7f1f8;
    --grey: #5c636b;  --grey-bg: #eef0f2;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #14171a; --panel: #1b1f23; --ink: #e6e9ec; --muted: #98a2b3;
      --line: #2a3038; --accent: #6f9bff;
      --green: #79d99b; --green-bg: #16301f;
      --amber: #e8c268; --amber-bg: #322709;
      --red: #f28b96;   --red-bg: #35151a;
      --blue: #8ec6ec;  --blue-bg: #10262f;
      --grey: #a8b0b8;  --grey-bg: #23282e;
    }
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; background: var(--bg); color: var(--ink);
    font: 15px/1.5 "Segoe UI", system-ui, -apple-system, sans-serif;
  }
  header {
    padding: 14px 20px; border-bottom: 1px solid var(--line);
    background: var(--panel); display: flex; align-items: baseline; gap: 14px;
    position: sticky; top: 0; z-index: 5;
  }
  header h1 { font-size: 16px; margin: 0; font-weight: 650; }
  header .sub { color: var(--muted); font-size: 13px; }
  main { max-width: 1100px; margin: 0 auto; padding: 20px; }
  .panel {
    background: var(--panel); border: 1px solid var(--line);
    border-radius: 10px; padding: 18px; margin-bottom: 18px;
  }
  .panel h2 {
    margin: 0 0 14px; font-size: 13px; letter-spacing: .04em;
    text-transform: uppercase; color: var(--muted); font-weight: 650;
  }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr)); gap: 14px; }
  label { display: block; font-size: 13px; color: var(--muted); margin-bottom: 5px; }
  input[type=text], input[type=number] {
    width: 100%; padding: 8px 10px; border: 1px solid var(--line);
    border-radius: 7px; background: var(--bg); color: var(--ink); font-size: 14px;
  }
  .check { display: flex; gap: 9px; align-items: flex-start; margin-top: 12px; font-size: 14px; }
  .check input { margin-top: 3px; flex: none; }
  button {
    font: inherit; font-weight: 600; padding: 9px 16px; border-radius: 7px;
    border: 1px solid var(--line); background: var(--panel); color: var(--ink);
    cursor: pointer;
  }
  button:hover { border-color: var(--accent); }
  button.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
  button.danger { color: var(--red); border-color: var(--red); }
  button:disabled { opacity: .5; cursor: default; }
  .row { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; margin-top: 16px; }
  .warn {
    margin-top: 14px; padding: 12px 14px; border-radius: 8px;
    background: var(--amber-bg); color: var(--amber);
    border: 1px solid color-mix(in srgb, var(--amber) 35%, transparent);
    font-size: 14px;
  }
  .tiles { display: flex; gap: 10px; flex-wrap: wrap; margin-bottom: 18px; }
  .tile {
    flex: 1 1 130px; background: var(--panel); border: 1px solid var(--line);
    border-radius: 10px; padding: 12px 14px;
  }
  .tile .n { font-size: 24px; font-weight: 680; line-height: 1.2; }
  .tile .k { font-size: 12px; color: var(--muted); text-transform: uppercase; letter-spacing: .04em; }
  .prompt {
    background: var(--blue-bg); border: 1px solid color-mix(in srgb, var(--blue) 35%, transparent);
    border-radius: 10px; padding: 16px 18px; margin-bottom: 18px;
  }
  .prompt p { margin: 0 0 12px; font-weight: 600; }
  table { width: 100%; border-collapse: collapse; font-size: 14px; }
  th {
    text-align: left; font-size: 12px; text-transform: uppercase;
    letter-spacing: .04em; color: var(--muted); padding: 0 10px 8px;
    border-bottom: 1px solid var(--line);
  }
  td { padding: 8px 10px; border-bottom: 1px solid var(--line); vertical-align: top; }
  td.num { font-variant-numeric: tabular-nums; white-space: nowrap; font-weight: 600; }
  td.detail { color: var(--muted); }
  .pill {
    display: inline-block; padding: 2px 9px; border-radius: 999px;
    font-size: 12px; font-weight: 650; white-space: nowrap;
  }
  .s-SENT { background: var(--green-bg); color: var(--green); }
  .s-SENT_UNVERIFIED { background: var(--amber-bg); color: var(--amber); }
  .s-SKIPPED { background: var(--amber-bg); color: var(--amber); }
  .s-REVIEW { background: var(--amber-bg); color: var(--amber); }
  .s-NOT_FOUND, .s-ERROR { background: var(--red-bg); color: var(--red); }
  .s-WORKING { background: var(--blue-bg); color: var(--blue); }
  .s-QUIT, .s-PENDING { background: var(--grey-bg); color: var(--grey); }
  pre#log {
    margin: 0; max-height: 340px; overflow: auto; font-size: 13px;
    font-family: Consolas, "Cascadia Mono", monospace; white-space: pre-wrap;
    color: var(--ink);
  }
  .files { margin-top: 12px; font-size: 14px; color: var(--muted); }
  .files code { color: var(--ink); }
  .pick { display: flex; gap: 8px; }
  .pick input { flex: 1; }
  .hint { font-size: 13px; color: var(--muted); margin: 7px 0 0; }
  .drop { outline: 2px dashed var(--accent); outline-offset: 6px; }
  .hide { display: none; }
  .err { color: var(--red); font-size: 14px; margin-top: 12px; }
</style>
</head>
<body>
<header>
  <h1>SOC night-shift follow-up</h1>
  <span class="sub" id="head-sub">not started</span>
</header>

<main>
  <section class="panel" id="setup">
    <h2>Run settings</h2>
    <div class="grid">
      <div style="grid-column: 1 / -1">
        <label for="sheet">Export to work through</label>
        <div class="pick">
          <input type="text" id="sheet" placeholder="Choose a file, or drag it anywhere onto this page">
          <button type="button" id="browse">Browse...</button>
        </div>
        <p class="hint">
          Use the <b>.xlsx</b> you filtered in Excel. Rows you filtered out are
          skipped, and the filter only survives inside a workbook.
        </p>
        <div class="warn hide" id="csvWarn">
          That is a <b>.csv</b>, and an Excel filter does not survive in one.
          Every row in it will be followed up, including the ones you filtered
          out. Save the filtered sheet as <b>.xlsx</b> if that matters.
        </div>
      </div>
      <div>
        <label for="column">Case-number column (blank = detect)</label>
        <input type="text" id="column" placeholder="auto">
      </div>
      <div>
        <label for="url">Mailbox URL</label>
        <input type="text" id="url" value="__URL__">
      </div>
      <div>
        <label for="limit">Stop after N cases (0 = all)</label>
        <input type="number" id="limit" value="0" min="0">
      </div>
      <div>
        <label for="settle">Search settle (seconds)</label>
        <input type="number" id="settle" value="3" step="0.5" min="0">
      </div>
      <div>
        <label for="sendDelay">Wait after a send (seconds)</label>
        <input type="number" id="sendDelay" value="5" step="0.5" min="0">
      </div>
    </div>

    <div class="check">
      <input type="checkbox" id="noPause">
      <label for="noPause" style="margin:0">Don't stop when a case has no matching mail</label>
    </div>
    <div class="check">
      <input type="checkbox" id="autoSend">
      <label for="autoSend" style="margin:0"><b>Send automatically</b> when the rule below matches</label>
    </div>

    <div id="autoBox" class="hide">
      <div class="grid" style="margin-top:12px">
        <div>
          <label for="autoSender">Only if the NEWEST mail is from</label>
          <input type="text" id="autoSender" value="__SENDER__" placeholder="name or address">
        </div>
        <div>
          <label for="autoPhrase">and the mail contains</label>
          <input type="text" id="autoPhrase" value="__PHRASE__" placeholder="follow up">
        </div>
      </div>
      <div class="warn">
        Auto-send replies with <b>no review</b>. The reply carries your Outlook
        signature exactly as it is set right now, and it goes to everyone on the
        thread. Check your signature before you turn this on.
      </div>
      <div class="check">
        <input type="checkbox" id="agreed">
        <label for="agreed" style="margin:0">
          I have checked my Outlook signature and I understand replies will be
          sent without me seeing them.
        </label>
      </div>
    </div>

    <div class="row">
      <button class="primary" id="start">Start run</button>
      <button id="check">Check export first</button>
    </div>
    <div class="err" id="setupErr"></div>
    <pre id="checkOut" class="hide" style="margin-top:14px;font-size:13px"></pre>
  </section>

  <div class="tiles hide" id="tiles">
    <div class="tile"><div class="n" id="t-done">0</div><div class="k">Processed</div></div>
    <div class="tile"><div class="n" id="t-replied" style="color:var(--green)">0</div><div class="k">Replied</div></div>
    <div class="tile"><div class="n" id="t-skipped" style="color:var(--amber)">0</div><div class="k">Skipped</div></div>
    <div class="tile"><div class="n" id="t-notfound" style="color:var(--red)">0</div><div class="k">Not found</div></div>
    <div class="tile"><div class="n" id="t-errored" style="color:var(--red)">0</div><div class="k">Errored</div></div>
  </div>

  <section class="prompt hide" id="promptBox">
    <p id="promptText"></p>
    <div class="row" style="margin:0" id="promptButtons"></div>
  </section>

  <section class="panel hide" id="tracker">
    <h2>Cases</h2>
    <table>
      <thead><tr><th style="width:120px">Case</th><th style="width:170px">Status</th><th>Detail</th></tr></thead>
      <tbody id="rows"></tbody>
    </table>
  </section>

  <section class="panel hide" id="logPanel">
    <h2>Log</h2>
    <pre id="log"></pre>
    <div class="files hide" id="files"></div>
    <div class="row">
      <button class="danger" id="stop">Stop after this case</button>
    </div>
  </section>
</main>

<script>
var TOKEN = "__TOKEN__";
var LABELS = {
  review:   { "": "Sent \u2713", "s": "Next case \u2192", "r": "Retry", "q": "Stop run" },
  notfound: { "": "Next case \u2192", "q": "Stop run" },
  verify:   { "": "Yes, it was sent", "s": "No \u2014 mark skipped", "r": "Retry" }
};
// The engine's prompt is written for a terminal, where the choices have to be
// spelled out. Here they are buttons, so say what happened instead.
var QUESTIONS = {
  review:   "The reply-all draft is open in Outlook. Review it and hit Send " +
            "there \u2014 this updates on its own when the draft closes.",
  notfound: "Nothing in the mailbox matches this case. Carry on?",
  verify:   "The draft closed, but no matching reply turned up in Sent Items. " +
            "Was it actually sent?"
};
var STARTED = false;
var lastLogLen = 0;

function el(id) { return document.getElementById(id); }

// Reads a response without assuming it is JSON. A proxy or an error page can
// answer with plain text, and silently swallowing that is how a failure turns
// into "nothing happened".
function apiRead(path, body) {
  return api(path, body).then(function (r) {
    return r.text().then(function (text) {
      var data = null;
      try { data = JSON.parse(text); } catch (e) { data = null; }
      if (data === null) {
        data = { error: (text || "").trim() || ("HTTP " + r.status) };
      }
      return { ok: r.ok, status: r.status, d: data };
    });
  }).catch(function (e) {
    return { ok: false, status: 0, d: { error: "could not reach the tool: " + e } };
  });
}

function api(path, body) {
  return fetch(path, {
    method: body === undefined ? "GET" : "POST",
    headers: { "Content-Type": "application/json", "X-Socfu-Token": TOKEN },
    body: body === undefined ? undefined : JSON.stringify(body)
  });
}

function isCsv(path) {
  var p = (path || "").trim().toLowerCase();
  return p.slice(-4) === ".csv";
}

function refreshSheetWarning() {
  el("csvWarn").classList.toggle("hide", !isCsv(el("sheet").value));
}

el("sheet").addEventListener("input", refreshSheetWarning);

el("browse").addEventListener("click", function () {
  var b = this;
  b.disabled = true;
  b.textContent = "Choosing...";
  el("setupErr").textContent = "";
  apiRead("/api/browse", {})
    .then(function (res) {
      if (res.d.path) { el("sheet").value = res.d.path; refreshSheetWarning(); }
      if (res.d.error) { el("setupErr").textContent = res.d.error; }
    })
    .catch(function () {})
    .then(function () { b.disabled = false; b.textContent = "Browse..."; });
});

// Dragging the export straight out of Downloads is quicker than any dialog.
// A browser will not hand a page the real path of a dropped file, so it is
// copied to the machine's temp directory and the engine reads it from there.
["dragenter", "dragover"].forEach(function (name) {
  document.addEventListener(name, function (e) {
    e.preventDefault();
    if (!STARTED) { el("setup").classList.add("drop"); }
  });
});
["dragleave", "drop"].forEach(function (name) {
  document.addEventListener(name, function (e) {
    e.preventDefault();
    el("setup").classList.remove("drop");
  });
});
document.addEventListener("drop", function (e) {
  if (STARTED) { return; }
  var files = e.dataTransfer && e.dataTransfer.files;
  if (!files || !files.length) { return; }
  var form = new FormData();
  form.append("sheet", files[0]);
  el("setupErr").textContent = "";
  // This was the one call that bypassed apiRead and had no catch at any link,
  // so a drop made while the tool was no longer listening failed in complete
  // silence and the analyst just dragged the file again.
  fetch("/api/upload", {
    method: "POST", headers: { "X-Socfu-Token": TOKEN }, body: form
  })
    .then(function (r) {
      return r.text().then(function (text) {
        var d = null;
        try { d = JSON.parse(text); } catch (e) { d = null; }
        if (d === null) { d = { error: (text || "").trim() || ("HTTP " + r.status) }; }
        return { ok: r.ok, d: d };
      });
    })
    .then(function (res) {
      if (!res.ok || res.d.error) {
        el("setupErr").textContent =
          res.d.error || "could not read that file";
        return;
      }
      el("sheet").value = res.d.path;
      refreshSheetWarning();
    })
    .catch(function (err) {
      el("setupErr").textContent =
        "the drop did not reach the tool (" + err + "). Is its window " +
        "still open?";
    });
});

el("autoSend").addEventListener("change", function () {
  el("autoBox").classList.toggle("hide", !this.checked);
});

el("check").addEventListener("click", function () {
  el("setupErr").textContent = "";
  apiRead("/api/check", { sheet: el("sheet").value, column: el("column").value })
    .then(function (res) {
      var out = el("checkOut");
      out.classList.remove("hide");
      if (!res.ok || res.d.error) {
        out.textContent = "Could not read it: " +
          (res.d.error || ("HTTP " + res.status));
        return;
      }
      out.textContent = res.d.report;
    });
});

el("start").addEventListener("click", function () {
  el("setupErr").textContent = "";
  var body = {
    sheet: el("sheet").value, column: el("column").value, url: el("url").value,
    limit: el("limit").value, settle: el("settle").value,
    sendDelay: el("sendDelay").value, noPause: el("noPause").checked,
    headless: false,
    autoSend: el("autoSend").checked, autoSender: el("autoSender").value,
    autoPhrase: el("autoPhrase").value, agreed: el("agreed").checked
  };
  apiRead("/api/start", body)
    .then(function (res) {
      if (!res.ok || res.d.error) {
        el("setupErr").textContent = res.d.error ||
          ("could not start (HTTP " + res.status + ")");
        return;
      }
      STARTED = true;
      el("setup").classList.add("hide");
      ["tiles", "tracker", "logPanel"].forEach(function (id) {
        el(id).classList.remove("hide");
      });
    });
});

el("stop").addEventListener("click", function () {
  this.disabled = true;
  api("/api/stop", {});
});

function statusText(s) {
  if (s === "SENT_UNVERIFIED") { return "Replied (unconfirmed)"; }
  if (s === "NOT_FOUND") { return "Not found"; }
  if (s === "SENT") { return "Replied"; }
  if (s === "WORKING") { return "Working..."; }
  if (s === "REVIEW") { return "Waiting for you"; }
  if (!s) { return "Pending"; }
  return s.charAt(0) + s.slice(1).toLowerCase();
}

function render(st) {
  el("t-done").textContent = st.cases.length + (st.total ? " / " + st.total : "");
  el("t-replied").textContent = st.replied;
  el("t-skipped").textContent = st.skipped;
  el("t-notfound").textContent = st.notFound;
  el("t-errored").textContent = st.errored;

  var sub = st.running ? "running" : (st.done ? "finished" : "not started");
  if (st.error) { sub = "stopped: " + st.error; }
  el("head-sub").textContent = sub;

  var body = el("rows");
  body.textContent = "";
  st.cases.forEach(function (c) {
    var tr = document.createElement("tr");
    var num = document.createElement("td");
    num.className = "num";
    num.textContent = "SOC" + c.num;
    var stat = document.createElement("td");
    var pill = document.createElement("span");
    pill.className = "pill s-" + (c.status || "PENDING");
    pill.textContent = statusText(c.status);
    stat.appendChild(pill);
    var det = document.createElement("td");
    det.className = "detail";
    det.textContent = c.detail || "";
    tr.appendChild(num); tr.appendChild(stat); tr.appendChild(det);
    body.appendChild(tr);
  });

  if (st.logs.length !== lastLogLen) {
    lastLogLen = st.logs.length;
    var log = el("log");
    log.textContent = st.logs.join("\n");
    log.scrollTop = log.scrollHeight;
  }

  var box = el("promptBox");
  if (st.prompt && st.prompt.active) {
    box.classList.remove("hide");
    el("promptText").textContent =
      QUESTIONS[st.prompt.kind] || st.prompt.text.trim();
    var holder = el("promptButtons");
    holder.textContent = "";
    var labels = LABELS[st.prompt.kind] || {};
    st.prompt.choices.forEach(function (choice, i) {
      var b = document.createElement("button");
      b.textContent = labels[choice] || (choice === "" ? "Continue" : choice);
      if (i === 0) { b.className = "primary"; }
      if (choice === "q") { b.className = "danger"; }
      b.addEventListener("click", function () {
        // Send the generation this button belongs to, so an answer for a
        // prompt that has already gone cannot answer the next one. Disable
        // them all on click: a double-click used to queue a second answer.
        api("/api/answer", { choice: choice, gen: st.prompt.gen });
        Array.prototype.forEach.call(
          holder.querySelectorAll("button"), function (x) { x.disabled = true; });
        box.classList.add("hide");
      });
      holder.appendChild(b);
    });
  } else {
    box.classList.add("hide");
  }

  if (st.files && st.files.length) {
    var f = el("files");
    f.classList.remove("hide");
    f.textContent = "Written: ";
    st.files.forEach(function (name, i) {
      var c = document.createElement("code");
      c.textContent = name;
      f.appendChild(c);
      if (i < st.files.length - 1) { f.appendChild(document.createTextNode(", ")); }
    });
  }
  if (st.done) { el("stop").disabled = true; }
}

function poll() {
  api("/api/state")
    .then(function (r) { return r.json(); })
    .then(function (st) { if (STARTED) { render(st); } })
    .catch(function () {})
    .then(function () { setTimeout(poll, 500); });
}
poll();
</script>
</body>
</html>`
