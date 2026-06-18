import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Vite 配置 React 探测前端，开发时通过页面表单连接 api-server WebSocket。
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
  },
});
