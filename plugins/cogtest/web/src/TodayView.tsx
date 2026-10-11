import { useState } from "react";
import { letter } from "./history";
import type { Anchor, Answer, Question, Summary, Today } from "./types";

interface TodayProps {
  today: Today;
  starting: boolean;
  onNewQuiz: () => void;
  onAnswer: (q: Question, chosen: number) => Promise<boolean>;
  onOpenFile: (path: string, line: number) => void;
}

export function TodayView({ today, starting, onNewQuiz, onAnswer, onOpenFile }: TodayProps) {
  const q = today.quiz;
  const empty = (lines: React.ReactNode, button?: string) => (
    <>
      <SummaryCard summary={today.summary} />
      <div className="empty">
        {lines}
        {button && (
          <button type="button" className="primary" disabled={starting} onClick={onNewQuiz}>
            {button}
          </button>
        )}
      </div>
    </>
  );
  if (!q) return empty(<p className="muted">No quiz yet. A quiz has a few questions about this channel's code.</p>, "Write a quiz");
  switch (q.status) {
    case "generating":
      return empty(<p className="muted">An agent is reading the code and writing questions. This takes a few minutes.</p>);
    case "failed":
      return empty(
        <>
          <p className="muted">The last quiz couldn't be written:</p>
          <p className="error-text">{q.error || "unknown error"}</p>
        </>,
        "Try again",
      );
    case "expired":
      return empty(<p className="muted">The last quiz took too long to write and was dropped.</p>, "Try again");
  }
  const s = q.score;
  return (
    <>
      <SummaryCard summary={today.summary} />
      <div className="summary">
        <span>Quiz of {q.date}</span>
        <span className="muted">
          {s.answered} of {s.total} answered
        </span>
        {s.answered > 0 && <span className="muted">{s.correct} correct</span>}
      </div>
      {(q.questions || []).map((qq, i) => (
        <QuestionCard key={qq.id} question={qq} number={i + 1} onAnswer={onAnswer} onOpenFile={onOpenFile} />
      ))}
    </>
  );
}

/** The streak, recent accuracy and what's due; nothing before the first
 *  answer. */
function SummaryCard({ summary: s }: { summary: Summary | null }) {
  if (!s || (!s.answered && !s.due_areas && !s.streak)) return null;
  return (
    <section className="summary-card" aria-label="Summary">
      <Stat value={String(s.streak)} label={s.streak === 1 ? "day streak" : "days streak"} title="Days in a row with at least one answer" />
      <Stat
        value={s.answered ? `${Math.round((100 * s.correct) / s.answered)}%` : "—"}
        label={`right, last ${s.days} days`}
        title={`${s.correct} of ${s.answered} answers`}
      />
      <Stat value={String(s.due_areas)} label={s.due_areas === 1 ? "area due" : "areas due"} title="Areas due for review; the next quiz focuses on them" />
      {s.weakest && <Stat value={s.weakest} label="weakest area" title="The area in the lowest box with a wrong answer" />}
    </section>
  );
}

function Stat({ value, label, title }: { value: string; label: string; title: string }) {
  return (
    <div className="stat" title={title}>
      <span className="stat-value">{value}</span>
      <span className="stat-label">{label}</span>
    </div>
  );
}

/** A question, answered or not. Without onAnswer it can't be answered, as
 *  in History. */
export function QuestionCard({
  question: q,
  number,
  onAnswer,
  onOpenFile,
}: {
  question: Question;
  number: number;
  onAnswer?: TodayProps["onAnswer"];
  onOpenFile: TodayProps["onOpenFile"];
}) {
  const [grading, setGrading] = useState(false);
  const a = q.answer;
  const choose = async (idx: number) => {
    if (!onAnswer) return;
    setGrading(true);
    if (!(await onAnswer(q, idx))) setGrading(false);
  };
  const choiceClass = (idx: number) => {
    if (!a) return "choice";
    if (idx === a.correct_index) return "choice correct";
    if (idx === a.chosen_index) return "choice wrong";
    return "choice";
  };
  return (
    <article className="card">
      <h3>
        Question {number}
        {q.area ? ` · ${q.area}` : ""}
        {q.reask && (
          <span className="badge" title="You missed this one before">
            Re-ask
          </span>
        )}
      </h3>
      <p className="question">{q.question}</p>
      <ol className="choices">
        {q.choices.map((text, idx) => (
          // The choices are fixed for a question: their index is their key.
          // biome-ignore lint/suspicious/noArrayIndexKey: see above
          <li key={idx}>
            <button type="button" className={choiceClass(idx)} disabled={!!a || grading || !onAnswer} onClick={() => choose(idx)}>
              {letter(idx)}. {text}
            </button>
          </li>
        ))}
      </ol>
      {a ? <Feedback answer={a} onOpenFile={onOpenFile} /> : !onAnswer && <p className="muted">Not answered.</p>}
    </article>
  );
}

function Feedback({ answer: a, onOpenFile }: { answer: Answer; onOpenFile: TodayProps["onOpenFile"] }) {
  return (
    <div className="feedback">
      {a.correct ? <p className="verdict correct">Correct.</p> : <p className="verdict wrong">Not quite. The answer is {letter(a.correct_index)}.</p>}
      {a.stale && (
        <p className="stale-note">
          {a.correct
            ? "The code changed since this question was written."
            : "The code changed since this question was written, so this answer doesn't count against the area."}
        </p>
      )}
      <p className="explanation">{a.explanation}</p>
      <div className="anchors">
        {(a.anchors || []).map((an) => (
          <button key={anchorText(an)} type="button" className="anchor" title="Open in the editor" onClick={() => onOpenFile(an.path, an.start)}>
            {anchorText(an)}
          </button>
        ))}
      </div>
    </div>
  );
}

function anchorText(an: Anchor): string {
  return `${an.path}:${an.start}${an.end && an.end !== an.start ? `-${an.end}` : ""}`;
}
