import js from "@eslint/js";
import { defineConfig } from "eslint/config";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

// A remote URL in the source: the panel's content security policy blocks
// anything outside the bundle, so ship the file instead. (\x2F is "/",
// which a selector's regex can't spell.)
const remoteURL = /^https?:\x2F\x2F/.source;

export default defineConfig(
  { ignores: ["dist", "node_modules"] },
  js.configs.recommended,
  tseslint.configs.recommended,
  reactHooks.configs.flat.recommended,
  {
    languageOptions: { globals: globals.browser },
    rules: {
      // The panel's content security policy blocks eval and its kin.
      "no-eval": "error",
      "no-implied-eval": "error",
      "no-new-func": "error",
      "no-restricted-syntax": [
        "error",
        { selector: `Literal[value=/${remoteURL}/]`, message: "Remote URLs don't load in a plugin panel; bundle the file." },
        { selector: `TemplateElement[value.raw=/${remoteURL}/]`, message: "Remote URLs don't load in a plugin panel; bundle the file." },
      ],
      // The frame has no network, storage or cookies: data goes through the
      // bridge, UI state through createStorage.
      "no-restricted-globals": [
        "error",
        { name: "fetch", message: "Call the plugin's /x API with bridge.api." },
        { name: "XMLHttpRequest", message: "Call the plugin's /x API with bridge.api." },
        { name: "WebSocket", message: "Listen for the plugin's events with bridge.events.on." },
        { name: "localStorage", message: "Use createStorage(bridge) from @radutopala/plugin-ui." },
        { name: "sessionStorage", message: "Use createStorage(bridge) from @radutopala/plugin-ui." },
        { name: "indexedDB", message: "Use bridge.state or the plugin's /x API." },
      ],
    },
  },
);
