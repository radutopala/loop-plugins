// Prints fields of a plugin.json, one per line:
//   node manifest.mjs plugins/cogtest/plugin.json id version
// plugin.json is JSON with comments and trailing commas, so they're dropped
// first, outside strings.
import { readFileSync } from "node:fs";

function parseJWCC(text) {
  let out = "";
  let i = 0;
  while (i < text.length) {
    const c = text[i];
    if (c === '"') {
      let j = i + 1;
      while (j < text.length && text[j] !== '"') j += text[j] === "\\" ? 2 : 1;
      out += text.slice(i, j + 1);
      i = j + 1;
    } else if (c === "/" && text[i + 1] === "/") {
      while (i < text.length && text[i] !== "\n") i++;
    } else if (c === "/" && text[i + 1] === "*") {
      const end = text.indexOf("*/", i + 2);
      i = end < 0 ? text.length : end + 2;
    } else {
      // A trailing comma: out ends outside a string here.
      if (c === "}" || c === "]") out = out.replace(/,(\s*)$/, "$1");
      out += c;
      i++;
    }
  }
  return JSON.parse(out);
}

const [file, ...fields] = process.argv.slice(2);
if (!file || fields.length === 0) {
  console.error("usage: node manifest.mjs <plugin.json> <field>...");
  process.exit(2);
}
const m = parseJWCC(readFileSync(file, "utf8"));
for (const f of fields) {
  if (typeof m[f] !== "string" || m[f] === "") {
    console.error(`${file}: ${f} is missing`);
    process.exit(1);
  }
  console.log(m[f]);
}
