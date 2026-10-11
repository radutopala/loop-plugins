// The shapes of the plugin's own /x API, as its Go server sends them.

export interface Anchor {
  path: string;
  start: number;
  end: number;
}

export interface Grade {
  correct: boolean;
  correct_index: number;
  explanation: string;
  anchors: Anchor[] | null;
  stale?: boolean;
}

export interface Answer extends Grade {
  chosen_index: number;
}

export interface Question {
  id: number;
  index: number;
  question: string;
  choices: string[];
  area: string;
  reask: boolean;
  answer?: Answer;
}

export interface Score {
  total: number;
  answered: number;
  correct: number;
}

export type QuizStatus = "generating" | "ready" | "failed" | "expired";

export interface Quiz {
  id: number;
  date: string;
  status: QuizStatus;
  error?: string;
  questions: Question[] | null;
  score: Score;
}

export interface Summary {
  streak: number;
  answered: number;
  correct: number;
  days: number;
  due_areas: number;
  weakest?: string;
}

export interface Today {
  quiz: Quiz | null;
  summary: Summary | null;
}

export interface HistoryQuiz extends Score {
  id: number;
  date: string;
  status: QuizStatus;
  error?: string;
}

export interface Mastery {
  area: string;
  box: number;
  max_box: number;
  due: string;
  correct: number;
  wrong: number;
}

export interface Day extends Score {
  date: string;
}

export interface History {
  quizzes: HistoryQuiz[] | null;
  mastery: Mastery[] | null;
  days: Day[] | null;
}

export interface Settings {
  model: string;
  effort: string;
}
