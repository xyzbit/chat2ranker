import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const rankApiURL = process.env.RANK_API_URL || "http://127.0.0.1:8787";
const rankControlURL = process.env.RANK_CONTROL_URL || "http://127.0.0.1:8788";

export default defineConfig({
  build: {
    outDir: "dist/client",
  },
  optimizeDeps: {
    include: ["react", "react-dom/client"],
  },
  server: {
    host: "0.0.0.0",
    allowedHosts: ["terminal.local"],
    warmup: {
      clientFiles: ["./src/main.jsx"],
    },
    proxy: {
      "/api": { target: rankApiURL, changeOrigin: true },
      "/control": { target: rankControlURL, changeOrigin: true },
    },
  },
  plugins: [react()],
});
