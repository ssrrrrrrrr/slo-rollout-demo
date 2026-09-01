import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"
import path from "path"

const releasePortalTarget =
  process.env.VITE_RELEASE_PORTAL_TARGET ?? "http://127.0.0.1:18090"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/api": {
        target: releasePortalTarget,
        changeOrigin: true,
      },
      "/healthz": {
        target: releasePortalTarget,
        changeOrigin: true,
      },
    },
  },
})
