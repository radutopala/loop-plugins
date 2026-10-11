import { describe, expect, it } from "vitest";
import { chartPoints, dateOf, dayNumber, dayTitle, heatmapCells, lastDay, letter, localDate, scoreText } from "./history";
import type { Day, HistoryQuiz } from "./types";

const day = (date: string, total: number, answered: number, correct: number): Day => ({ date, total, answered, correct });

describe("dayNumber and dateOf", () => {
  it.each(["1970-01-01", "2026-03-29", "2026-10-11", "2028-02-29"])("round-trips %s", (date) => {
    expect(dateOf(dayNumber(date))).toBe(date);
  });

  it("counts days across a DST change", () => {
    expect(dayNumber("2026-03-30") - dayNumber("2026-03-28")).toBe(2);
  });
});

describe("localDate", () => {
  it("pads month and day", () => {
    expect(localDate(new Date(2026, 0, 5))).toBe("2026-01-05");
  });
});

describe("lastDay", () => {
  it.each([
    { name: "today", days: [day("2026-10-01", 3, 3, 2)], want: "2026-10-11" },
    { name: "a later quiz", days: [day("2026-10-12", 3, 0, 0)], want: "2026-10-12" },
    { name: "no days", days: [], want: "2026-10-11" },
  ])("is $name", ({ days, want }) => {
    expect(dateOf(lastDay(days, "2026-10-11"))).toBe(want);
  });
});

describe("dayTitle", () => {
  it.each([
    { d: undefined, want: "no quiz" },
    { d: day("2026-10-01", 4, 0, 0), want: "4 questions, none answered" },
    { d: day("2026-10-01", 4, 4, 3), want: "3 of 4 right" },
    { d: day("2026-10-01", 4, 2, 1), want: "1 of 2 right (4 asked)" },
  ])("says $want", ({ d, want }) => {
    expect(dayTitle(d)).toBe(want);
  });
});

describe("scoreText", () => {
  const q = (total: number, answered: number, correct: number): HistoryQuiz => ({ id: 1, date: "2026-10-01", status: "ready", total, answered, correct });
  it.each([
    { q: q(5, 0, 0), want: "not started (5 questions)" },
    { q: q(5, 5, 4), want: "4 / 5" },
    { q: q(5, 2, 2), want: "2 / 5 (2 answered)" },
  ])("says $want", ({ q, want }) => {
    expect(scoreText(q)).toBe(want);
  });
});

describe("letter", () => {
  it("names choices from A", () => {
    expect([0, 1, 4].map(letter)).toEqual(["A", "B", "E"]);
  });
});

describe("heatmapCells", () => {
  // 2026-10-11 is a Sunday: the last column ends on it.
  const today = "2026-10-11";

  it("lays out twelve whole weeks starting on a Monday", () => {
    const cells = heatmapCells([], today);
    expect(cells).toHaveLength(84);
    expect(cells[0].date).toBe("2026-07-20");
    expect(new Date(`${cells[0].date}T00:00:00Z`).getUTCDay()).toBe(1);
    expect(cells[83].date).toBe(today);
  });

  it("hides the days after today in its week", () => {
    const cells = heatmapCells([], "2026-10-08");
    expect(cells.slice(-3).map((c) => c.level)).toEqual(["future", "future", "future"]);
    expect(cells.at(-1)?.title).toBeUndefined();
    expect(cells.at(-4)?.level).toBe("none");
    expect(cells.at(-4)?.title).toBe("2026-10-08: no quiz");
  });

  it.each([
    { d: day(today, 4, 0, 0), want: "open" },
    { d: day(today, 4, 4, 0), want: "l1" },
    { d: day(today, 4, 4, 1), want: "l2" },
    { d: day(today, 4, 4, 2), want: "l3" },
    { d: day(today, 4, 4, 3), want: "l4" },
    { d: day(today, 4, 4, 4), want: "l4" },
  ])("shades $d.correct of $d.answered as $want", ({ d, want }) => {
    expect(heatmapCells([d], today).at(-1)?.level).toBe(want);
  });
});

describe("chartPoints", () => {
  const today = "2026-10-11";

  it("places answered days only, oldest left and today right", () => {
    const pts = chartPoints([day("2026-07-20", 4, 4, 4), day("2026-09-01", 4, 0, 0), day(today, 4, 4, 2)], today, 600, 120, 8);
    expect(pts.map((p) => p.day.date)).toEqual(["2026-07-20", today]);
    expect(pts[0]).toMatchObject({ x: 8, y: 8 });
    expect(pts[1]).toMatchObject({ x: 592, y: 60 });
  });
});
