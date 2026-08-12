import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig(({ command }) => ({
  root: "frontend",
  base: command === "serve" ? "/" : "/app/",
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: true,
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: false,
    proxy: {
      "/api": {
        target: "http://localhost:3000",
        changeOrigin: true,
        cookieDomainRewrite: "",
      },
      "/user-login": {
        target: "http://localhost:3000",
        changeOrigin: true,
        cookieDomainRewrite: "",
      },
      "/assets": {
        target: "http://localhost:3000",
        changeOrigin: true,
      },
      "/language": {
        target: "http://localhost:3000",
        changeOrigin: true,
        cookieDomainRewrite: "",
      },
    },
  },
}));
