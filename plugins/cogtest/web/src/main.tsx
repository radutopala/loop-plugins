import { createRoot } from "react-dom/client";
import { App } from "./App";
import { listen } from "./bridge";

// Listen before rendering: Loop hands the port over as the frame loads.
const port = listen();
const root = document.getElementById("root");
if (root) createRoot(root).render(<App port={port} />);
