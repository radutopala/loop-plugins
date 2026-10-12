import { describe, expect, it } from "vitest";
import { ago, plural } from "./format";
import { parseRoute, routeHash } from "./route";

const now = new Date("2026-01-02T12:00:00Z");

describe("ago", () => {
  it.each([
    ["2026-01-02T11:59:30Z", "just now"],
    ["2026-01-02T11:55:00Z", "5 min ago"],
    ["2026-01-02T09:00:00Z", "3 h ago"],
    ["2025-12-31T12:00:00Z", "2 d ago"],
    ["not a date", ""],
  ])("%s is %j", (iso, want) => {
    expect(ago(iso, now)).toBe(want);
  });
});

describe("plural", () => {
  it.each([
    [0, "0 files"],
    [1, "1 file"],
    [2, "2 files"],
  ])("%i", (n, want) => {
    expect(plural(n, "file")).toBe(want);
  });
});

describe("parseRoute", () => {
  it.each([
    ["", "count"],
    ["#/", "count"],
    ["#/about", "about"],
    ["#about", "about"],
    ["#/nope", "count"],
  ])("%j is %s", (hash, want) => {
    expect(parseRoute(hash)).toBe(want);
  });

  it("round-trips routeHash", () => {
    expect(parseRoute(routeHash("about"))).toBe("about");
  });
});
