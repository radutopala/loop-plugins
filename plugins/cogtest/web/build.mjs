// Builds the panel into dist/: the static files as they are, and the app as
// one classic script, since the panel's CSP allows scripts only from the
// bundle and its sandboxed frame can't load module scripts across origins.
import { cpSync, mkdirSync, rmSync } from "node:fs";
import { build } from "esbuild";

const out = "dist/quiz";
rmSync("dist", { recursive: true, force: true });
mkdirSync(out, { recursive: true });
cpSync("static", out, { recursive: true });
await build({
  entryPoints: ["src/main.tsx"],
  outfile: `${out}/app.js`,
  bundle: true,
  format: "iife",
  minify: true,
  target: "es2022",
  jsx: "automatic",
  define: { "process.env.NODE_ENV": '"production"' },
  legalComments: "none",
});
