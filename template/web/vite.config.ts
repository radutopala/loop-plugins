import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The panel loads from loop-plugin://<id>/ui/main/, so asset URLs have to be
// relative. Vite emits script files, never inline scripts, as the panel's
// content security policy requires.
export default defineConfig({
  base: "./",
  plugins: [react()],
  build: { outDir: "dist", emptyOutDir: true },
});
