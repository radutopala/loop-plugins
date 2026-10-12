// The summary card: draws the quiz results Loop hands over in context.get
// and sizes its frame to them.
import { applyTheme, Bridge, listen, type Theme } from "./bridge";
import { boxText, type CardData, type CardQuestion, dateText, fileText, moveKind, parseCard, scoreText, streakText } from "./card";
import { areaLabel } from "./title";

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className = "", text = ""): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  if (className) e.className = className;
  if (text) e.textContent = text;
  return e;
}

function question(q: CardQuestion, openFile: (path: string, line?: number) => void): HTMLElement {
  const li = el("li", `question ${q.correct ? "right" : "wrong"}`);
  li.append(el("span", "mark", q.correct ? "✓" : "✗"));
  const text = el("span", "text", q.question);
  text.title = q.question;
  li.append(text);
  const where = el("span", "where");
  where.append(el("span", "", q.reask ? `${areaLabel(q.area)} · asked again` : areaLabel(q.area)));
  if (q.path) {
    const file = el("button", "file", fileText(q));
    file.type = "button";
    file.title = `Open ${fileText(q)}`;
    const { path, line } = q;
    file.addEventListener("click", () => openFile(path, line));
    where.append(file);
  }
  li.append(where);
  return li;
}

function render(root: HTMLElement, card: CardData | null, openFile: (path: string, line?: number) => void) {
  root.replaceChildren();
  if (!card) {
    root.append(el("p", "muted", "No quiz results to show."));
    return;
  }
  const head = el("div", "head");
  head.append(el("span", "score", scoreText(card.score)), el("span", "muted", `right · ${dateText(card.date)}`));
  const streak = streakText(card.streak);
  if (streak) head.append(el("span", "streak", streak));
  root.append(head);

  const list = el("ul", "questions");
  for (const q of card.questions) list.append(question(q, openFile));
  root.append(list);

  if (card.moves.length) {
    const moves = el("div", "moves");
    moves.title = "Leitner boxes";
    for (const m of card.moves) {
      const chip = el("span", `move ${moveKind(m)}`, `${areaLabel(m.area)} ${boxText(m.from, card.max_box)} `);
      chip.append(el("span", "arrow", "→"), ` ${boxText(m.to, card.max_box)}`);
      moves.append(chip);
    }
    root.append(moves);
  }
}

// Listen before anything else: Loop hands the port over as the frame loads.
const port = listen();
const root = document.getElementById("root");

port.then(async (p) => {
  const bridge = new Bridge(p, { onTheme: applyTheme, onEvent: () => {} });
  bridge.call<Theme>("theme.get").then(applyTheme, () => {});
  let sent = 0;
  const resize = () => {
    const height = Math.ceil(document.body.scrollHeight);
    if (height === sent) return;
    sent = height;
    bridge.call("ui.resize", { height }).catch(() => {});
  };
  const openFile = (path: string, line?: number) => {
    bridge.call("ui.open_file", line ? { path, line } : { path }).catch(() => {});
  };
  let data: unknown = null;
  try {
    data = (await bridge.call<{ data?: unknown } | null>("context.get"))?.data;
  } catch {
    // Drawn without data below.
  }
  if (root) render(root, parseCard(data), openFile);
  resize();
  new ResizeObserver(resize).observe(document.body);
});
