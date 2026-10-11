// The History tab's arithmetic. Dates are the plugin's YYYY-MM-DD quiz
// dates; they're handled as UTC days so the arithmetic has no DST steps.
import type { Day, HistoryQuiz } from "./types";

// HEATMAP_WEEKS matches the 84 days the history sends.
export const HEATMAP_WEEKS = 12;
const DAY_MS = 86400000;

export function dayNumber(date: string): number {
  return Math.round(Date.parse(`${date}T00:00:00Z`) / DAY_MS);
}

export function dateOf(n: number): string {
  return new Date(n * DAY_MS).toISOString().slice(0, 10);
}

export function localDate(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** The last day the history shows: today, or a later quiz date. */
export function lastDay(days: Day[], today: string): number {
  return Math.max(dayNumber(today), ...days.map((d) => dayNumber(d.date)));
}

export function dayTitle(d: Day | undefined): string {
  if (!d) return "no quiz";
  if (!d.answered) return `${d.total} questions, none answered`;
  return `${d.correct} of ${d.answered} right${d.answered < d.total ? ` (${d.total} asked)` : ""}`;
}

export function scoreText(q: HistoryQuiz): string {
  if (!q.answered) return `not started (${q.total} questions)`;
  return `${q.correct} / ${q.total}${q.answered < q.total ? ` (${q.answered} answered)` : ""}`;
}

export function letter(i: number): string {
  return String.fromCharCode(65 + i);
}

export type CellLevel = "none" | "future" | "open" | "l1" | "l2" | "l3" | "l4";

export interface HeatCell {
  date: string;
  level: CellLevel;
  title: string | undefined;
}

/** The heatmap's cells, a week per column with Monday on top; a day's
 *  level is its share of right answers. */
export function heatmapCells(days: Day[], today: string): HeatCell[] {
  const byDate = new Map(days.map((d) => [d.date, d]));
  const end = lastDay(days, today);
  // Day 0 (1970-01-01) was a Thursday: (n + 3) % 7 is 0 on Mondays.
  const endCol = end - ((end + 3) % 7) + 6;
  const start = endCol - HEATMAP_WEEKS * 7 + 1;
  const cells: HeatCell[] = [];
  for (let n = start; n <= endCol; n++) {
    const date = dateOf(n);
    const d = byDate.get(date);
    let level: CellLevel = "none";
    if (n > end) level = "future";
    else if (d?.answered) level = `l${Math.min(4, 1 + Math.floor((4 * d.correct) / d.answered))}` as CellLevel;
    else if (d) level = "open";
    cells.push({ date, level, title: n > end ? undefined : `${date}: ${dayTitle(d)}` });
  }
  return cells;
}

export interface ChartPoint {
  day: Day;
  x: number;
  y: number;
}

/** Places the answered days on a width × height chart of the share of
 *  right answers, inset by pad. */
export function chartPoints(days: Day[], today: string, width: number, height: number, pad: number): ChartPoint[] {
  const span = HEATMAP_WEEKS * 7 - 1;
  const end = lastDay(days, today);
  return days
    .filter((d) => d.answered > 0)
    .map((d) => ({
      day: d,
      x: pad + ((width - 2 * pad) * (span - (end - dayNumber(d.date)))) / span,
      y: chartY(d.correct / d.answered, height, pad),
    }));
}

export function chartY(ratio: number, height: number, pad: number): number {
  return pad + (height - 2 * pad) * (1 - ratio);
}
