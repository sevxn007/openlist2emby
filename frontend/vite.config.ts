import react from "@vitejs/plugin-react"
import { defineConfig, loadEnv } from "vite"
import { fileURLToPath } from "node:url"

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "")
  const backend = env.VITE_OPENLIST2EMBY_BASE || "http://127.0.0.1:8091"

  const backendProxy = {
    target: backend,
    changeOrigin: true,
  }

  return {
    plugins: [react()],
    resolve: {
      alias: {
        "@": fileURLToPath(new URL("./src", import.meta.url)),
      },
    },
    server: {
      host: "0.0.0.0",
      port: 5174,
      strictPort: true,
      proxy: {
        "/api/manager": backendProxy,
      },
    },
    test: {
      environment: "jsdom",
      globals: true,
      setupFiles: "./src/test/setup.ts",
      css: true,
    },
  }
})
