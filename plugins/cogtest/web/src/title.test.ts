import { describe, expect, it } from "vitest";
import { areaLabel, paneTitle } from "./title";
import type { Quiz, QuizStatus, Today } from "./types";

const today = (status: QuizStatus, total: number, answered: number): Today => ({
  quiz: { id: 1, date: "2026-10-12", status, questions: null, score: { total, answered, correct: 0 } } satisfies Quiz,
  summary: null,
});

describe("paneTitle", () => {
  it.each([
    { name: "nothing loaded", today: undefined, want: "" },
    { name: "no quiz", today: { quiz: null, summary: null }, want: "" },
    { name: "a quiz being written", today: today("generating", 0, 0), want: "Quiz · writing" },
    { name: "questions left", today: today("ready", 10, 7), want: "Quiz · 3 left" },
    { name: "every question answered", today: today("ready", 10, 10), want: "" },
    { name: "a failed quiz", today: today("failed", 0, 0), want: "" },
  ])("$name", ({ today, want }) => {
    expect(paneTitle(today)).toBe(want);
  });
});

describe("areaLabel", () => {
  it.each([
    [".", "top level"],
    ["internal/store", "internal/store"],
  ])("%s", (area, want) => {
    expect(areaLabel(area)).toBe(want);
  });
});
