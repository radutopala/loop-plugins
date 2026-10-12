/** The plugin's GET /x/stats answer. */
export interface Stats {
  last?: { glob: string; files: number; truncated?: boolean; at: string };
  runs: number;
}
