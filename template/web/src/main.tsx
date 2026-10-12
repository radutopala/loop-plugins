import { type Bridge, connect, createStorage, syncTheme } from "@loop/plugin-ui";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import "./app.css";

// Connect before anything awaits: Loop hands the port over as the frame
// loads, and a listener added later misses it.
const connecting = connect();

async function start(bridge: Bridge) {
  await syncTheme(bridge);
  // The frame has no localStorage; this stand-in keeps the panel's UI state
  // in Loop, per channel. Pass it to libraries that persist through one.
  const storage = await createStorage(bridge);
  const root = document.getElementById("root");
  if (root) createRoot(root).render(<App bridge={bridge} storage={storage} />);
}

connecting.then(start).catch((err: unknown) => {
  document.body.textContent = `Couldn't start: ${err instanceof Error ? err.message : String(err)}`;
});
