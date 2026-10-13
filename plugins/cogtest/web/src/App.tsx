import { ApiError, type ApiResponse, type Bridge, type PluginEvent, syncTheme } from "@radutopala/loop-plugin-ui";
import { useCallback, useEffect, useRef, useState } from "react";
import { HistoryView } from "./HistoryView";
import { TodayView } from "./TodayView";
import { paneTitle } from "./title";
import type { Grade, History, Question, Quiz, Settings, Today } from "./types";

const POLL_MS = 20000;

type Tab = "today" | "history";

interface Status {
  text: string;
  error: boolean;
}

/** A view's data: loading (undefined), loaded, or the error it failed with. */
type Loaded<T> = { data: T } | { error: string } | undefined;

function expectOK<T>(res: ApiResponse<T>, ...ok: number[]): T {
  if (!(ok.length ? ok : [200]).includes(res.status)) throw new ApiError(res.status, res.body);
  return res.body;
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export function App({ bridge: connecting }: { bridge: Promise<Bridge> }) {
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
      setToday({ data: await b.api.get<Today>("quizzes/current") });
    } catch (err) {
      setToday({ error: message(err) });
    }
  }, []);

  const loadHistory = useCallback(async (b: Bridge) => {
    try {
      setHistory({ data: await b.api.get<History>("history") });
    } catch (err) {
      setHistory({ error: message(err) });
    }
  }, []);

  const loadQuiz = useCallback(
    async (id: number) => {
      if (!bridge) throw new Error("not connected to Loop");
      return bridge.api.get<Quiz>(`quizzes/${id}`);
    },
    [bridge],
  );

  const loadSettings = useCallback(
    async (b: Bridge) => {
      try {
        setSettings(await b.api.get<Settings>("settings"));
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
    connecting.then(async (b) => {
      if (cancelled) return;
      b.events.on((ev) => onEvent.current(ev));
      syncTheme(b).catch(() => {});
      let first: Tab = "today";
      try {
        const saved = await b.state.get<{ tab?: string }>();
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
  }, [connecting, loadCurrent, loadHistory, loadSettings]);

  // While a quiz generates, poll in case the ready event is missed.
  // Each load of today's quiz arms the next poll.
  const generating = !!today && "data" in today && today.data.quiz?.status === "generating";
  useEffect(() => {
    if (!bridge || !today || !("data" in today) || today.data.quiz?.status !== "generating") return;
    const timer = setTimeout(() => loadCurrent(bridge), POLL_MS);
    return () => clearTimeout(timer);
  }, [bridge, today, loadCurrent]);

  // The pane's header tells how today's quiz stands; a Loop without
  // ui.set_title just keeps the panel's label.
  const title = paneTitle(today && "data" in today ? today.data : null);
  useEffect(() => {
    bridge?.ui.setTitle(title).catch(() => {});
  }, [bridge, title]);

  // Today's quiz on screen clears its badge on the channel's row: once per
  // quiz, and only while the pane shows, not from a pane in a hidden tab.
  const todayRef = useRef<HTMLElement>(null);
  const seenQuiz = useRef<number | null>(null);
  const readyQuiz = today && "data" in today && today.data.quiz?.status === "ready" ? today.data.quiz.id : null;
  useEffect(() => {
    const el = todayRef.current;
    if (!bridge || !el || tab !== "today" || readyQuiz === null || seenQuiz.current === readyQuiz) return;
    const io = new IntersectionObserver((entries) => {
      if (seenQuiz.current === readyQuiz || !entries.some((e) => e.isIntersecting)) return;
      seenQuiz.current = readyQuiz;
      bridge.api.post("quizzes/current/seen").catch(() => {});
    });
    io.observe(el);
    return () => io.disconnect();
  }, [bridge, tab, readyQuiz]);

  const selectTab = (next: Tab) => {
    setTab(next);
    if (!bridge) return;
    bridge.state.set({ tab: next }).catch(() => {});
    if (next === "history") loadHistory(bridge);
  };

  const saveSettings = async (next: Settings) => {
    if (!bridge) return;
    setSettings(next);
    try {
      await bridge.api.put("settings", next);
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
      const res = await bridge.api.request("POST", "quizzes", {});
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
      const res = await bridge.api.request<Grade>("POST", "attempts", { question_id: q.id, chosen_index: chosen });
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
      bridge.api.request<Today>("GET", "quizzes/current").then(
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
      await bridge.ui.openFile(path, line);
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
        <section ref={todayRef} id="view-today" role="tabpanel" aria-label="Today" hidden={tab !== "today"}>
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
            <HistoryView history={history.data} loadQuiz={loadQuiz} onOpenFile={openFile} />
          )}
        </section>
      </main>
      <p id="status" className={status.error ? "status error" : "status"} role="status" aria-live="polite">
        {status.text}
      </p>
    </>
  );
}
