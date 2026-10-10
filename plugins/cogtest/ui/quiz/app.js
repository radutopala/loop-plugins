// cogtest panel. It runs in Loop's sandboxed plugin frame and talks to Loop
// only through the MessagePort bridge (JSON-RPC 2.0) handed over in the
// "loop-plugin:init" message.
(function () {
  "use strict";

  const CALL_TIMEOUT_MS = 35000;
  const POLL_MS = 20000;

  let port = null;
  let nextId = 1;
  const pending = new Map();
  const ui = {
    tab: "today",
    quiz: null,
    history: null,
    pollTimer: 0,
  };

  const $ = (id) => document.getElementById(id);

  // h builds an element: h("p", {class: "x", onclick: fn}, "text", child).
  function h(tag, attrs, ...children) {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v === undefined || v === null || v === false) continue;
      if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
      else if (k === "class") el.className = v;
      else el.setAttribute(k, v === true ? "" : String(v));
    }
    for (const c of children.flat()) {
      if (c === undefined || c === null || c === false) continue;
      el.append(c instanceof Node ? c : document.createTextNode(String(c)));
    }
    return el;
  }

  // Bridge.

  window.addEventListener("message", (event) => {
    const data = event.data;
    if (port || !data || data.type !== "loop-plugin:init" || !event.ports || !event.ports[0]) return;
    port = event.ports[0];
    port.onmessage = onPortMessage;
    init();
  });

  function onPortMessage(event) {
    let msg = event.data;
    if (typeof msg === "string") {
      try {
        msg = JSON.parse(msg);
      } catch {
        return;
      }
    }
    if (!msg || msg.jsonrpc !== "2.0") return;
    if (msg.id !== undefined && msg.id !== null) {
      const p = pending.get(msg.id);
      if (!p) return;
      pending.delete(msg.id);
      clearTimeout(p.timer);
      if (msg.error) p.reject(new Error(msg.error.message || "bridge error"));
      else p.resolve(msg.result);
      return;
    }
    if (msg.method === "theme") applyTheme(msg.params);
    else if (msg.method === "event") onPluginEvent(msg.params || {});
  }

  function call(method, params) {
    return new Promise((resolve, reject) => {
      if (!port) {
        reject(new Error("not connected to Loop"));
        return;
      }
      const id = nextId++;
      const timer = setTimeout(() => {
        pending.delete(id);
        reject(new Error(method + " timed out"));
      }, CALL_TIMEOUT_MS);
      pending.set(id, { resolve, reject, timer });
      const msg = { jsonrpc: "2.0", id, method };
      if (params !== undefined) msg.params = params;
      port.postMessage(msg);
    });
  }

  // api calls the plugin's own /x API and returns {status, body}.
  async function api(method, path, body) {
    const params = { method, path };
    if (body !== undefined) params.body = body;
    const res = await call("api.request", params);
    return res || { status: 0, body: null };
  }

  function errorOf(res) {
    const b = res && res.body;
    if (b && typeof b === "object" && b.error) return b.error;
    if (typeof b === "string" && b) return b;
    return "request failed (" + (res ? res.status : "no answer") + ")";
  }

  // Theme.

  function applyTheme(theme) {
    if (!theme) return;
    const root = document.documentElement;
    if (theme.mode === "dark" || theme.mode === "light") root.dataset.mode = theme.mode;
    for (const [k, v] of Object.entries(theme.vars || {})) {
      if (k.startsWith("--") && typeof v === "string") root.style.setProperty(k, v);
    }
  }

  // Status line.

  let statusTimer = 0;
  function status(text, isError) {
    const el = $("status");
    el.textContent = text || "";
    el.classList.toggle("error", !!isError);
    clearTimeout(statusTimer);
    if (text) statusTimer = setTimeout(() => status(""), isError ? 8000 : 4000);
  }

  // Start-up.

  async function init() {
    for (const b of document.querySelectorAll(".tabs button")) {
      b.addEventListener("click", () => selectTab(b.dataset.tab, true));
    }
    $("new-quiz").addEventListener("click", newQuiz);
    $("model").addEventListener("change", saveSettings);
    $("effort").addEventListener("change", saveSettings);

    call("theme.get").then(applyTheme, () => {});
    try {
      const saved = await call("state.get");
      if (saved && (saved.tab === "today" || saved.tab === "history")) ui.tab = saved.tab;
    } catch {
      // No saved state; keep the default tab.
    }
    selectTab(ui.tab, false);
    await Promise.all([loadSettings(), refresh()]);
  }

  function selectTab(tab, remember) {
    ui.tab = tab;
    for (const b of document.querySelectorAll(".tabs button")) {
      b.setAttribute("aria-selected", String(b.dataset.tab === tab));
    }
    $("view-today").hidden = tab !== "today";
    $("view-history").hidden = tab !== "history";
    if (remember) {
      call("state.set", { value: { tab } }).catch(() => {});
      if (tab === "history") loadHistory();
    }
  }

  async function refresh() {
    await loadCurrent();
    if (ui.tab === "history") await loadHistory();
  }

  function onPluginEvent(ev) {
    if (ev.type === "quiz_ready" || ev.type === "quiz_failed" || ev.type === "quiz_started") {
      if (ev.type === "quiz_ready") status("Today's quiz is ready.");
      refresh();
    }
  }

  // Settings.

  async function loadSettings() {
    try {
      const res = await api("GET", "settings");
      if (res.status !== 200) throw new Error(errorOf(res));
      $("model").value = res.body.model || "";
      $("effort").value = res.body.effort || "";
    } catch (err) {
      status("Couldn't load settings: " + err.message, true);
    } finally {
      $("model").disabled = false;
      $("effort").disabled = false;
    }
  }

  async function saveSettings() {
    const body = { model: $("model").value, effort: $("effort").value };
    try {
      const res = await api("PUT", "settings", body);
      if (res.status !== 200) throw new Error(errorOf(res));
      status("Saved. The next quiz uses these settings.");
    } catch (err) {
      status("Couldn't save settings: " + err.message, true);
      loadSettings();
    }
  }

  // Today.

  async function loadCurrent() {
    try {
      const res = await api("GET", "quizzes/current");
      if (res.status !== 200) throw new Error(errorOf(res));
      ui.quiz = res.body.quiz;
      renderToday();
    } catch (err) {
      renderMessage($("view-today"), "Couldn't load the quiz: " + err.message, true);
    }
    schedulePoll();
  }

  // While a quiz generates, poll in case the ready event is missed.
  function schedulePoll() {
    clearTimeout(ui.pollTimer);
    if (ui.quiz && ui.quiz.status === "generating") ui.pollTimer = setTimeout(loadCurrent, POLL_MS);
  }

  async function newQuiz() {
    const btn = $("new-quiz");
    btn.disabled = true;
    try {
      const res = await api("POST", "quizzes", {});
      if (res.status === 409) status("A quiz is already being written.");
      else if (res.status !== 202 && res.status !== 200) throw new Error(errorOf(res));
      else status("Writing a new quiz…");
    } catch (err) {
      status("Couldn't start a quiz: " + err.message, true);
    }
    await loadCurrent();
  }

  function renderMessage(view, text, isError) {
    view.replaceChildren(h("p", { class: isError ? "error-text" : "muted" }, text));
  }

  function renderEmpty(view, lines, buttonLabel) {
    view.replaceChildren(
      h(
        "div",
        { class: "empty" },
        lines.map((l) => (l instanceof Node ? l : h("p", { class: "muted" }, l))),
        buttonLabel && h("button", { type: "button", class: "primary", onclick: newQuiz }, buttonLabel),
      ),
    );
  }

  function renderToday() {
    const view = $("view-today");
    const q = ui.quiz;
    $("new-quiz").disabled = !!q && q.status === "generating";
    if (!q) {
      renderEmpty(view, ["No quiz yet. A quiz has a few questions about this channel's code."], "Write a quiz");
      return;
    }
    switch (q.status) {
      case "generating":
        renderEmpty(view, ["An agent is reading the code and writing questions. This takes a few minutes."]);
        return;
      case "failed":
        renderEmpty(view, ["The last quiz couldn't be written:", h("p", { class: "error-text" }, q.error || "unknown error")], "Try again");
        return;
      case "expired":
        renderEmpty(view, ["The last quiz took too long to write and was dropped."], "Try again");
        return;
    }
    const s = q.score || { total: 0, answered: 0, correct: 0 };
    const summary = h(
      "div",
      { class: "summary" },
      h("span", null, "Quiz of " + q.date),
      h("span", { class: "muted" }, s.answered + " of " + s.total + " answered"),
      s.answered > 0 && h("span", { class: "muted" }, s.correct + " correct"),
    );
    const cards = q.questions.map((qq, i) => renderQuestion(qq, i));
    view.replaceChildren(summary, ...cards);
  }

  function renderQuestion(q, i) {
    const card = h("article", { class: "card" });
    const choices = h("ol", { class: "choices" });
    const buttons = q.choices.map((text, idx) => {
      const b = h("button", { type: "button", class: "choice", onclick: () => answer(q, idx, card) }, letter(idx) + ". " + text);
      choices.append(h("li", null, b));
      return b;
    });
    card.append(h("h3", null, "Question " + (i + 1) + (q.area ? " · " + q.area : "")), h("p", { class: "question" }, q.question), choices);
    if (q.answer) showAnswer(card, buttons, q.answer);
    return card;
  }

  function letter(i) {
    return String.fromCharCode(65 + i);
  }

  async function answer(q, idx, card) {
    const buttons = Array.from(card.querySelectorAll(".choice"));
    for (const b of buttons) b.disabled = true;
    try {
      const res = await api("POST", "attempts", { question_id: q.id, chosen_index: idx });
      if (res.status === 409) {
        await loadCurrent();
        return;
      }
      if (res.status !== 200) throw new Error(errorOf(res));
      q.answer = Object.assign({ chosen_index: idx }, res.body);
      showAnswer(card, buttons, q.answer);
      updateScore(res.body.correct);
    } catch (err) {
      for (const b of buttons) b.disabled = false;
      status("Couldn't grade the answer: " + err.message, true);
    }
  }

  function updateScore(correct) {
    const s = ui.quiz && ui.quiz.score;
    if (!s) return;
    s.answered += 1;
    if (correct) s.correct += 1;
    const summary = $("view-today").querySelector(".summary");
    if (summary) {
      summary.replaceWith(
        h(
          "div",
          { class: "summary" },
          h("span", null, "Quiz of " + ui.quiz.date),
          h("span", { class: "muted" }, s.answered + " of " + s.total + " answered"),
          h("span", { class: "muted" }, s.correct + " correct"),
        ),
      );
    }
  }

  function showAnswer(card, buttons, a) {
    buttons.forEach((b, idx) => {
      b.disabled = true;
      if (idx === a.correct_index) b.classList.add("correct");
      else if (idx === a.chosen_index) b.classList.add("wrong");
    });
    const verdict = a.correct
      ? h("p", { class: "verdict correct" }, "Correct.")
      : h("p", { class: "verdict wrong" }, "Not quite. The answer is " + letter(a.correct_index) + ".");
    const anchors = h(
      "div",
      { class: "anchors" },
      (a.anchors || []).map((an) =>
        h(
          "button",
          { type: "button", class: "anchor", title: "Open in the editor", onclick: () => openFile(an) },
          an.path + ":" + an.start + (an.end && an.end !== an.start ? "-" + an.end : ""),
        ),
      ),
    );
    const old = card.querySelector(".feedback");
    if (old) old.remove();
    card.append(h("div", { class: "feedback" }, verdict, h("p", { class: "explanation" }, a.explanation || ""), anchors));
  }

  async function openFile(anchor) {
    try {
      await call("ui.open_file", { path: anchor.path, line: anchor.start });
    } catch (err) {
      status("Couldn't open " + anchor.path + ": " + err.message, true);
    }
  }

  // History.

  async function loadHistory() {
    const view = $("view-history");
    try {
      const res = await api("GET", "history");
      if (res.status !== 200) throw new Error(errorOf(res));
      ui.history = res.body;
      renderHistory();
    } catch (err) {
      renderMessage(view, "Couldn't load the history: " + err.message, true);
    }
  }

  function renderHistory() {
    const view = $("view-history");
    const hist = ui.history || { quizzes: [], mastery: [] };
    const parts = [h("h2", null, "Quizzes")];
    if (!hist.quizzes.length) parts.push(h("p", { class: "muted" }, "No quizzes yet."));
    else {
      parts.push(
        h(
          "table",
          null,
          h("thead", null, h("tr", null, h("th", null, "Date"), h("th", null, "Status"), h("th", null, "Score"))),
          h(
            "tbody",
            null,
            hist.quizzes.map((q) =>
              h(
                "tr",
                null,
                h("td", null, q.date),
                h("td", { title: q.error || null }, q.status),
                h("td", null, q.status === "ready" ? scoreText(q) : "—"),
              ),
            ),
          ),
        ),
      );
    }
    parts.push(h("h2", null, "Mastery by area"));
    if (!hist.mastery.length) parts.push(h("p", { class: "muted" }, "Answer questions to track mastery."));
    else {
      parts.push(
        h(
          "table",
          null,
          h(
            "thead",
            null,
            h("tr", null, h("th", null, "Area"), h("th", null, "Box"), h("th", null, "Right / wrong"), h("th", null, "Due")),
          ),
          h(
            "tbody",
            null,
            hist.mastery.map((m) =>
              h(
                "tr",
                null,
                h("td", null, m.area),
                h("td", { title: "Box " + m.box + " of " + m.max_box }, boxes(m.box, m.max_box)),
                h("td", null, m.correct + " / " + m.wrong),
                h("td", null, m.due),
              ),
            ),
          ),
        ),
      );
    }
    view.replaceChildren(...parts);
  }

  function scoreText(q) {
    if (!q.answered) return "not started (" + q.total + " questions)";
    return q.correct + " / " + q.total + (q.answered < q.total ? " (" + q.answered + " answered)" : "");
  }

  function boxes(box, max) {
    const out = h("span", { class: "boxes" });
    for (let i = 1; i <= (max || 5); i++) out.append(h("span", { class: i <= box ? "on" : "" }));
    return out;
  }
})();
