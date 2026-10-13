import { connect } from "@radutopala/loop-plugin-ui";
import { createRoot } from "react-dom/client";
import { App } from "./App";

// Connect before rendering: Loop hands the port over as the frame loads.
const bridge = connect();
const root = document.getElementById("root");
if (root) createRoot(root).render(<App bridge={bridge} />);
