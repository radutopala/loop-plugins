import type { Today } from "./types";

/** The name the panel gives its pane: how today's quiz stands while it's
 *  being written or has questions left, else "" for the panel's label. */
export function paneTitle(today: Today | null | undefined): string {
  const quiz = today?.quiz;
  if (quiz?.status === "generating") return "Quiz · writing";
  if (quiz?.status !== "ready") return "";
  const left = quiz.score.total - quiz.score.answered;
  return left > 0 ? `Quiz · ${left} left` : "";
}

/** An area as the panel and card show it: the repo's top level has the dir
 *  ".", which reads as nothing. */
export function areaLabel(area: string): string {
  return area === "." ? "top level" : area;
}
