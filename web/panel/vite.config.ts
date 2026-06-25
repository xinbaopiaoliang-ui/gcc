import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    chunkSizeWarningLimit: 1200
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    proxy: {
      "/api": {
        target: process.env.VITE_PANEL_API_PROXY ?? "http://127.0.0.1:8090",
        changeOrigin: true
      },
      "/health": {
        target: process.env.VITE_PANEL_API_PROXY ?? "http://127.0.0.1:8090",
        changeOrigin: true
      }
    }
  }
});
