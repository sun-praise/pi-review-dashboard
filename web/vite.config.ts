import { defineConfig } from "vite";

export default defineConfig({
  // Dev-time proxy: vite serves the UI on :5173 and forwards API calls to
  // the Go binary. In production both are same-origin inside one binary.
  server: { proxy: { "/api": "http://localhost:8787" } },
  build: { outDir: "dist", emptyOutDir: true },
});
