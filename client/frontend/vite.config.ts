import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

export default defineConfig({
  base: "./",
  plugins: [vue()],
  build: {
    // tsc emits the framework-free modules to dist/ for node --test; keep the
    // webview bundle separate so both pipelines can run in one build.
    outDir: "dist-web",
    emptyOutDir: true,
  },
});
