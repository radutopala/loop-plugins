// Builds the panel and the summary card into dist/: each page's static
// files as they are, and its script as one classic script, since the CSP
// allows scripts only from the bundle and the sandboxed frame can't load
// module scripts across origins.
import { cpSync, mkdirSync, rmSync } from "node:fs";
import { build } from "esbuild";

const pages = [
  { out: "dist/quiz", static: "static", entry: "src/main.tsx", script: "app.js" },
  { out: "dist/card", static: "card", entry: "src/cardMain.ts", script: "card.js" },
];

rmSync("dist", { recursive: true, force: true });
for (const page of pages) {
  mkdirSync(page.out, { recursive: true });
  cpSync(page.static, page.out, { recursive: true });
  await build({
    entryPoints: [page.entry],
    outfile: `${page.out}/${page.script}`,
    bundle: true,
    format: "iife",
    minify: true,
    target: "es2022",
    jsx: "automatic",
    define: { "process.env.NODE_ENV": '"production"' },
    legalComments: "none",
  });
}
