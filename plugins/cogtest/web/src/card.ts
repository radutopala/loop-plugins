// The summary card's data, as the Go service posts it, and the text the
// card shows for it.

export interface CardScore {
  total: number;
  answered: number;
  correct: number;
}

export interface CardQuestion {
  question: string;
  area: string;
  path?: string;
  line?: number;
  correct: boolean;
  reask: boolean;
}

/** The Leitner box an area went from and to; 0 is a new area. */
export interface BoxMove {
  area: string;
  from: number;
  to: number;
}

export interface CardData {
  quiz_id: number;
  date: string;
  score: CardScore;
  streak: number;
  max_box: number;
  questions: CardQuestion[];
  moves: BoxMove[];
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function num(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

/** The card's data from context.get, or null when it isn't a summary. */
export function parseCard(data: unknown): CardData | null {
  if (!isObject(data) || !isObject(data.score)) return null;
  const questions: CardQuestion[] = (Array.isArray(data.questions) ? data.questions : []).filter(isObject).map((q) => {
    const out: CardQuestion = { question: str(q.question), area: str(q.area), correct: q.correct === true, reask: q.reask === true };
    if (str(q.path)) {
      out.path = str(q.path);
      if (num(q.line) > 0) out.line = num(q.line);
    }
    return out;
  });
  const moves: BoxMove[] = (Array.isArray(data.moves) ? data.moves : []).filter(isObject).map((m) => ({ area: str(m.area), from: num(m.from), to: num(m.to) }));
  return {
    quiz_id: num(data.quiz_id),
    date: str(data.date),
    score: { total: num(data.score.total), answered: num(data.score.answered), correct: num(data.score.correct) },
    streak: num(data.streak),
    max_box: num(data.max_box),
    questions,
    moves,
  };
}

/** "Oct 12" for "2026-10-12"; anything else as it is. */
export function dateText(date: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  const month = m ? MONTHS[Number(m[2]) - 1] : undefined;
  if (!m || !month) return date;
  return `${month} ${Number(m[3])}`;
}

export function scoreText(s: CardScore): string {
  return `${s.correct}/${s.total}`;
}

/** The streak badge, or "" without one. */
export function streakText(days: number): string {
  if (days <= 0) return "";
  return days === 1 ? "1 day streak" : `${days} days in a row`;
}

export function boxText(box: number, max: number): string {
  if (box <= 0) return "new";
  return max > 0 ? `${box}/${max}` : String(box);
}

export type MoveKind = "up" | "down" | "same";

export function moveKind(m: BoxMove): MoveKind {
  if (m.to > m.from) return "up";
  return m.to < m.from ? "down" : "same";
}

/** "pkg/a.go:12", or the path alone without a line. */
export function fileText(q: CardQuestion): string {
  if (!q.path) return "";
  return q.line ? `${q.path}:${q.line}` : q.path;
}
