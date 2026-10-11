import { Fragment, useLayoutEffect, useRef, useState } from "react";
import { chartPoints, chartY, dayTitle, HEATMAP_WEEKS, heatmapCells, localDate, scoreText } from "./history";
import { QuestionCard } from "./TodayView";
import type { Day, History, HistoryQuiz, Quiz } from "./types";

interface HistoryProps {
  history: History;
  /** Loads a past quiz with its questions and answers. */
  loadQuiz: (id: number) => Promise<Quiz>;
  onOpenFile: (path: string, line: number) => void;
}

export function HistoryView({ history, loadQuiz, onOpenFile }: HistoryProps) {
  const days = history.days || [];
  const quizzes = history.quizzes || [];
  const mastery = history.mastery || [];
  const today = localDate(new Date());
  return (
    <>
      {days.some((d) => d.answered > 0) && (
        <>
          <h2>Accuracy</h2>
          <TrendChart days={days} today={today} />
          <h2>Last {HEATMAP_WEEKS} weeks</h2>
          <Heatmap days={days} today={today} />
        </>
      )}
      <h2>Quizzes</h2>
      {!quizzes.length ? (
        <p className="muted">No quizzes yet.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Date</th>
              <th>Status</th>
              <th>Score</th>
            </tr>
          </thead>
          <tbody>
            {quizzes.map((q) => (
              <QuizRow key={q.id} quiz={q} loadQuiz={loadQuiz} onOpenFile={onOpenFile} />
            ))}
          </tbody>
        </table>
      )}
      <h2>Mastery by area</h2>
      {!mastery.length ? (
        <p className="muted">Answer questions to track mastery.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Area</th>
              <th>Box</th>
              <th>Right / wrong</th>
              <th>Due</th>
            </tr>
          </thead>
          <tbody>
            {mastery.map((m) => (
              <tr key={m.area}>
                <td>{m.area}</td>
                <td title={`Box ${m.box} of ${m.max_box}`}>
                  <Boxes box={m.box} max={m.max_box || 5} />
                </td>
                <td>
                  {m.correct} / {m.wrong}
                </td>
                <td>{m.due}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}

/** A past quiz's row; a ready one opens below it to its questions, what was
 *  answered and the right answers. */
function QuizRow({ quiz: q, loadQuiz, onOpenFile }: { quiz: HistoryQuiz } & Omit<HistoryProps, "history">) {
  const [open, setOpen] = useState(false);
  const [detail, setDetail] = useState<{ quiz: Quiz } | { error: string } | null>(null);
  const ready = q.status === "ready";
  const toggle = () => {
    setOpen(!open);
    // Loaded on each opening, so answers given since show.
    if (open) return;
    setDetail(null);
    loadQuiz(q.id)
      .then((quiz) => setDetail({ quiz }))
      .catch((err) => setDetail({ error: err instanceof Error ? err.message : String(err) }));
  };
  return (
    <Fragment>
      <tr className={ready ? "quiz-row" : undefined} aria-expanded={ready ? open : undefined} onClick={ready ? toggle : undefined}>
        <td>
          {ready && <span className="disclosure">{open ? "▾" : "▸"}</span>}
          {q.date}
        </td>
        <td title={q.error || undefined}>{q.status}</td>
        <td>{ready ? scoreText(q) : "—"}</td>
      </tr>
      {open && (
        <tr className="quiz-detail">
          <td colSpan={3}>
            {!detail ? (
              <p className="muted">Loading…</p>
            ) : "error" in detail ? (
              <p className="error-text">Couldn't load the quiz: {detail.error}</p>
            ) : (
              (detail.quiz.questions || []).map((qq, i) => <QuestionCard key={qq.id} question={qq} number={i + 1} onOpenFile={onOpenFile} />)
            )}
          </td>
        </tr>
      )}
    </Fragment>
  );
}

const CHART_H = 140;
const CHART_PAD = 8;

/** The share of right answers per answered day, as wide as the panel. */
function TrendChart({ days, today }: { days: Day[]; today: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(600);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => setWidth(Math.max(200, Math.floor(el.clientWidth)));
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const points = chartPoints(days, today, width, CHART_H, CHART_PAD);
  const y = (r: number) => chartY(r, CHART_H, CHART_PAD);
  return (
    <div ref={ref} className="chart-wrap">
      <svg
        className="chart"
        width={width}
        height={CHART_H}
        viewBox={`0 0 ${width} ${CHART_H}`}
        role="img"
        aria-label={`Share of right answers per day, last ${HEATMAP_WEEKS} weeks`}
      >
        {[0, 0.5, 1].map((r) => (
          <line key={r} className="grid" x1={CHART_PAD} x2={width - CHART_PAD} y1={y(r)} y2={y(r)} />
        ))}
        <text className="axis" x={CHART_PAD} y={y(1) - 2}>
          100%
        </text>
        <text className="axis" x={CHART_PAD} y={y(0.5) - 2}>
          50%
        </text>
        {points.length > 1 && <polyline className="trend" points={points.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ")} />}
        {points.map((p) => (
          <circle key={p.day.date} className="dot" cx={p.x.toFixed(1)} cy={p.y.toFixed(1)} r={3.5}>
            <title>{`${p.day.date}: ${dayTitle(p.day)}`}</title>
          </circle>
        ))}
      </svg>
    </div>
  );
}

function Heatmap({ days, today }: { days: Day[]; today: string }) {
  return (
    <div className="heatmap-wrap">
      <div className="heatmap" role="img" aria-label={`Right answers per day, last ${HEATMAP_WEEKS} weeks`}>
        {heatmapCells(days, today).map((c) => (
          <span key={c.date} className={`cell ${c.level}`} title={c.title} />
        ))}
      </div>
      <div className="legend muted">
        fewer right
        {(["l1", "l2", "l3", "l4"] as const).map((l) => (
          <span key={l} className={`cell ${l}`} />
        ))}
        more right
      </div>
    </div>
  );
}

function Boxes({ box, max }: { box: number; max: number }) {
  return (
    <span className="boxes">
      {Array.from({ length: max }, (_, i) => (
        // The boxes are positions: their index is their key.
        // biome-ignore lint/suspicious/noArrayIndexKey: see above
        <span key={i} className={i + 1 <= box ? "on" : ""} />
      ))}
    </span>
  );
}
