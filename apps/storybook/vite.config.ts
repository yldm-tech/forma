import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import path from "path";
import { defineConfig, lazyPlugins } from "vite-plus";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: lazyPlugins(() => [react(), tailwindcss()]),
  define: {
    "process.env": {},
  },
  resolve: {
    alias: {
      "@forma/survey-ui": path.resolve(__dirname, "../../packages/survey-ui/src"),
    },
  },
});
