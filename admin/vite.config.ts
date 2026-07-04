import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    // 本機開發（不經 nginx）時把 /api 轉給 compose 內的 api——
    // 需要臨時把 api 的 8070 映出來；正式路徑一律走 nginx
    proxy: { "/api": "http://localhost:8070" },
  },
});
