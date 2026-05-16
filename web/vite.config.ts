import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

// `server.proxy` is dev-only; production builds ignore it. Vitest merges this file as a plain object.
const devEnv = loadEnv('development', process.cwd(), '')
const nexusOrigin = devEnv.VITE_DEV_NEXUS_ORIGIN || 'http://127.0.0.1:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/public': {
        target: nexusOrigin,
        changeOrigin: true,
      },
    },
  },
})
