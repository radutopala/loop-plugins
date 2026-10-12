import { describe, expect, it } from "vitest";
import { boxText, dateText, fileText, moveKind, parseCard, scoreText, streakText } from "./card";

const posted = {
  quiz_id: 7,
  date: "2026-10-12",
  score: { total: 3, answered: 3, correct: 2 },
  streak: 2,
  max_box: 5,
  questions: [
    { question: "What does A do?", area: "pkg", path: "pkg/a.go", line: 3, correct: false, reask: false },
    { question: "B?", area: "web", path: "web/b.ts", line: 10, correct: true, reask: false },
    { question: "Old?", area: "pkg", correct: true, reask: true },
  ],
  moves: [
    { area: "pkg", from: 1, to: 1 },
    { area: "web", from: 0, to: 2 },
  ],
};

describe("parseCard", () => {
  it("takes the posted card as it is", () => {
    expect(parseCard(posted)).toEqual(posted);
  });

  it.each([
    { name: "null", data: null },
    { name: "an array", data: [] },
    { name: "a string", data: "x" },
    { name: "no score", data: { quiz_id: 1 } },
  ])("is null for $name", ({ data }) => {
    expect(parseCard(data)).toBeNull();
  });

  it("fills in what is missing or malformed", () => {
    expect(
      parseCard({
        score: { total: "3" },
        streak: Number.NaN,
        questions: [null, { question: 4, area: "a", path: "x.go", line: 0, correct: "yes" }, { path: "", line: 3 }],
        moves: "none",
      }),
    ).toEqual({
      quiz_id: 0,
      date: "",
      score: { total: 0, answered: 0, correct: 0 },
      streak: 0,
      max_box: 0,
      questions: [
        { question: "", area: "a", path: "x.go", correct: false, reask: false },
        { question: "", area: "", correct: false, reask: false },
      ],
      moves: [],
    });
  });
});

describe("text", () => {
  it.each([
    ["2026-10-12", "Oct 12"],
    ["2026-01-05", "Jan 5"],
    ["2026-13-05", "2026-13-05"],
    ["today", "today"],
  ])("dateText(%s) is %s", (date, want) => {
    expect(dateText(date)).toBe(want);
  });

  it("scoreText", () => {
    expect(scoreText({ total: 5, answered: 5, correct: 4 })).toBe("4/5");
  });

  it.each([
    [0, ""],
    [-1, ""],
    [1, "1 day streak"],
    [4, "4 days in a row"],
  ])("streakText(%i) is %j", (days, want) => {
    expect(streakText(days)).toBe(want);
  });

  it.each([
    [0, 5, "new"],
    [2, 5, "2/5"],
    [3, 0, "3"],
  ])("boxText(%i, %i) is %s", (box, max, want) => {
    expect(boxText(box, max)).toBe(want);
  });

  it.each([
    [{ area: "a", from: 1, to: 2 }, "up"],
    [{ area: "a", from: 0, to: 1 }, "up"],
    [{ area: "a", from: 3, to: 1 }, "down"],
    [{ area: "a", from: 1, to: 1 }, "same"],
  ] as const)("moveKind(%j) is %s", (m, want) => {
    expect(moveKind(m)).toBe(want);
  });

  it.each([
    [{ question: "q", area: "a", path: "a.go", line: 3, correct: true, reask: false }, "a.go:3"],
    [{ question: "q", area: "a", path: "a.go", correct: true, reask: false }, "a.go"],
    [{ question: "q", area: "a", correct: true, reask: false }, ""],
  ])("fileText(%j) is %s", (q, want) => {
    expect(fileText(q)).toBe(want);
  });
});
