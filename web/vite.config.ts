import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8080",
      "/export.ics": "http://127.0.0.1:8080",
      "/view/data": "http://127.0.0.1:8080",
    },
  },
});
