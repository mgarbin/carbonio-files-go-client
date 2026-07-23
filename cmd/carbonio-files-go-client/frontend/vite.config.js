import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Wails serves the built frontend from an embedded filesystem (see
// ../main.go's `go:embed all:frontend/dist`), not from a domain root, so
// every asset reference must be relative - hence `base: "./"`.
export default defineConfig({
  plugins: [svelte()],
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
