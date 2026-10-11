import { useCallback, useEffect, useRef, useState } from "react";
import { type ApiResponse, applyTheme, Bridge, errorOf, type PluginEvent } from "./bridge";
import { HistoryView } from "./HistoryView";
import { TodayView } from "./TodayView";
import type { Grade, History, Question, Settings, Today } from "./types";

const POLL_MS = 20000;

type Tab = "today" | "history";

interface Status {
  text: string;
  error: boolean;
}

/** A view's data: loading (undefined), loaded, or the error it failed with. */
type Loaded<T> = { data: T } | { error: string } | undefined;

async function expectOK<T>(res: ApiResponse<T>, ...ok: number[]): Promise<T> {
  if (!(ok.length ? ok : [200]).includes(res.status)) throw new Error(errorOf(res));
  return res.body;
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export function App({ port }: { port: Promise<MessagePort> }) {
  const [bridge, setBridge] = useState<Bridge | null>(null);
  const [tab, setTab] = useState<Tab>("today");
  const [today, setToday] = useState<Loaded<Today>>();
  const [history, setHistory] = useState<Loaded<History>>();
  const [settings, setSettings] = useState<Settings | null>(null);
  const [starting, setStarting] = useState(false);
  const [status, setStatusState] = useState<Status>({ text: "", error: false });

  const statusTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const showStatus = useCallback((text: string, error = false) => {
    setStatusState({ text, error });
    clearTimeout(statusTimer.current);
    if (text) statusTimer.current = setTimeout(() => setStatusState({ text: "", error: false }), error ? 8000 : 4000);
  }, []);

  const loadCurrent = useCallback(async (b: Bridge) => {
    try {
      setToday({ data: await expectOK(await b.api<Today>("GET", "quizzes/current")) });
    } catch (err) {
      setToday({ error: message(err) });
    }
  }, []);

  const loadHistory = useCallback(async (b: Bridge) => {
    try {
      setHistory({ data: await expectOK(await b.api<History>("GET", "history")) });
    } catch (err) {
      setHistory({ error: message(err) });
    }
  }, []);

  const loadSettings = useCallback(
    async (b: Bridge) => {
      try {
        setSettings(await expectOK(await b.api<Settings>("GET", "settings")));
      } catch (err) {
        setSettings({ model: "", effort: "" });
        showStatus(`Couldn't load settings: ${message(err)}`, true);
      }
    },
    [showStatus],
  );

  // Events come in through the bridge, set up once: they reach the latest
  // render through a ref.
  const onEvent = useRef<(ev: PluginEvent) => void>(() => {});
  onEvent.current = (ev) => {
    if (!bridge || !(ev.type === "quiz_ready" || ev.type === "quiz_failed" || ev.type === "quiz_started")) return;
    if (ev.type === "quiz_ready") showStatus("Today's quiz is ready.");
    loadCurrent(bridge);
    if (tab === "history") loadHistory(bridge);
  };

  useEffect(() => {
    let cancelled = false;
    port.then(async (p) => {
      if (cancelled) return;
      const b = new Bridge(p, { onTheme: applyTheme, onEvent: (ev) => onEvent.current(ev) });
      b.call<Parameters<typeof applyTheme>[0]>("theme.get").then(applyTheme, () => {});
      let first: Tab = "today";
      try {
        const saved = await b.call<{ tab?: string } | null>("state.get");
        if (saved?.tab === "today" || saved?.tab === "history") first = saved.tab;
      } catch {
        // No saved state; keep the default tab.
      }
      setTab(first);
      setBridge(b);
      loadSettings(b);
      loadCurrent(b);
      if (first === "history") loadHistory(b);
    });
    return () => {
      cancelled = true;
    };
  }, [port, loadCurrent, loadHistory, loadSettings]);

  // While a quiz generates, poll in case the ready event is missed.
  // Each load of today's quiz arms the next poll.
  const generating = !!today && "data" in today && today.data.quiz?.status === "generating";
  useEffect(() => {
    if (!bridge || !today || !("data" in today) || today.data.quiz?.status !== "generating") return;
    const timer = setTimeout(() => loadCurrent(bridge), POLL_MS);
    return () => clearTimeout(timer);
  }, [bridge, today, loadCurrent]);

  const selectTab = (next: Tab) => {
    setTab(next);
    if (!bridge) return;
    bridge.call("state.set", { value: { tab: next } }).catch(() => {});
    if (next === "history") loadHistory(bridge);
  };

  const saveSettings = async (next: Settings) => {
    if (!bridge) return;
    setSettings(next);
    try {
      await expectOK(await bridge.api("PUT", "settings", next));
      showStatus("Saved. The next quiz uses these settings.");
    } catch (err) {
      showStatus(`Couldn't save settings: ${message(err)}`, true);
      loadSettings(bridge);
    }
  };

  const newQuiz = async () => {
    if (!bridge) return;
    setStarting(true);
    try {
      const res = await bridge.api("POST", "quizzes", {});
      if (res.status === 409) showStatus("A quiz is already being written.");
      else {
        await expectOK(res, 200, 202);
        showStatus("Writing a new quiz…");
      }
    } catch (err) {
      showStatus(`Couldn't start a quiz: ${message(err)}`, true);
    }
    await loadCurrent(bridge);
    setStarting(false);
  };

  // answer grades a choice; it resolves false when the answer didn't land.
  const answer = async (q: Question, chosen: number): Promise<boolean> => {
    if (!bridge) return false;
    try {
      const res = await bridge.api<Grade>("POST", "attempts", { question_id: q.id, chosen_index: chosen });
      if (res.status === 409) {
        await loadCurrent(bridge);
        return true;
      }
      const grade = await expectOK(res);
      setToday((cur) => {
        if (!cur || !("data" in cur) || !cur.data.quiz) return cur;
        const quiz = cur.data.quiz;
        const score = { ...quiz.score, answered: quiz.score.answered + 1, correct: quiz.score.correct + (grade.correct ? 1 : 0) };
        const questions = (quiz.questions || []).map((qq) => (qq.id === q.id ? { ...qq, answer: { ...grade, chosen_index: chosen } } : qq));
        return { data: { ...cur.data, quiz: { ...quiz, score, questions } } };
      });
      // The summary card follows the answer.
      bridge.api<Today>("GET", "quizzes/current").then(
        (r) => {
          if (r.status !== 200) return;
          setToday((cur) => (cur && "data" in cur ? { data: { ...cur.data, summary: r.body.summary } } : cur));
        },
        () => {},
      );
      return true;
    } catch (err) {
      showStatus(`Couldn't grade the answer: ${message(err)}`, true);
      return false;
    }
  };

  const openFile = async (path: string, line: number) => {
    if (!bridge) return;
    try {
      await bridge.call("ui.open_file", { path, line });
    } catch (err) {
      showStatus(`Couldn't open ${path}: ${message(err)}`, true);
    }
  };

  return (
    <>
      <header className="bar">
        <div className="tabs" role="tablist" aria-label="Views">
          {(["today", "history"] as const).map((t) => (
            <button key={t} type="button" role="tab" data-tab={t} aria-selected={tab === t} onClick={() => selectTab(t)}>
              {t === "today" ? "Today" : "History"}
            </button>
          ))}
        </div>
        <div className="controls">
          <label>
            Model
            <select
              id="model"
              disabled={!settings}
              value={settings?.model ?? ""}
              onChange={(e) => settings && saveSettings({ ...settings, model: e.target.value })}
            >
              <option value="">Channel default</option>
              <option value="sonnet">Sonnet</option>
              <option value="opus">Opus</option>
              <option value="haiku">Haiku</option>
            </select>
          </label>
          <label>
            Effort
            <select
              id="effort"
              disabled={!settings}
              value={settings?.effort ?? ""}
              onChange={(e) => settings && saveSettings({ ...settings, effort: e.target.value })}
            >
              <option value="">Channel default</option>
              <option value="low">Low</option>
              <option value="medium">Medium</option>
              <option value="high">High</option>
            </select>
          </label>
          <button type="button" id="new-quiz" className="primary" disabled={!bridge || !today || starting || generating} onClick={newQuiz}>
            New quiz
          </button>
        </div>
      </header>
      <main>
        <section id="view-today" role="tabpanel" aria-label="Today" hidden={tab !== "today"}>
          {!today ? (
            <p className="muted">{bridge ? "Loading…" : "Connecting to Loop…"}</p>
          ) : "error" in today ? (
            <p className="error-text">Couldn't load the quiz: {today.error}</p>
          ) : (
            <TodayView today={today.data} starting={starting} onNewQuiz={newQuiz} onAnswer={answer} onOpenFile={openFile} />
          )}
        </section>
        <section id="view-history" role="tabpanel" aria-label="History" hidden={tab !== "history"}>
          {!history ? null : "error" in history ? (
            <p className="error-text">Couldn't load the history: {history.error}</p>
          ) : (
            <HistoryView history={history.data} />
          )}
        </section>
      </main>
      <p id="status" className={status.error ? "status error" : "status"} role="status" aria-live="polite">
        {status.text}
      </p>
    </>
  );
}
