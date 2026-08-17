import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig(({ command, mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const backendTarget = env.VITE_BACKEND_URL || "http://127.0.0.1:3001";

  // Keep page navigation on Vite so `/inbox` and the other SPA entry points do
  // not fall through to the Go server's embedded production bundle. Backend
  // routes are listed explicitly and proxied to the Go process instead.
  const backendProxy = {
    target: backendTarget,
    changeOrigin: true,
    cookieDomainRewrite: "",
  };

  return {
    root: "frontend",
    base: command === "serve" ? "/" : "/app/",
    resolve: {
      alias: {
        "@": new URL("./src", import.meta.url).pathname,
      },
    },
    plugins: [react(), tailwindcss()],
    build: {
      outDir: "dist",
      emptyOutDir: true,
      sourcemap: true,
    },
    server: {
      host: "127.0.0.1",
      port: 3000,
      strictPort: true,
      proxy: {
        "/api": backendProxy,
        "/v1": backendProxy,
        "/assets": backendProxy,
        "/csrf": backendProxy,
        "/user-login": backendProxy,
        "/login": backendProxy,
        "/register": backendProxy,
        "/demo-login": backendProxy,
        "/logout": backendProxy,
        "/language": backendProxy,
        "/auth": backendProxy,
        "/settings": backendProxy,
        "/events": backendProxy,
        "/sw.js": backendProxy,
        "/licenses.txt": backendProxy,
        "/health": backendProxy,
      },
    },
  };
});
