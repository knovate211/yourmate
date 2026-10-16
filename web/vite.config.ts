import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// In dev the gateway runs on :8095; proxying keeps the app same-origin so the
// SameSite=Strict refresh cookie works exactly as it does behind nginx.
const gateway = process.env.VITE_GATEWAY ?? 'http://localhost:8095'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    rollupOptions: {
      output: {
        // Libraries change far less often than the app, so they get their own
        // long-cached file and a deploy only invalidates the app's code.
        manualChunks: { vendor: ['react', 'react-dom', 'react-router-dom', 'graphql-ws'] },
      },
    },
  },
  server: {
    port: 5191,
    proxy: {
      '/graphql': { target: gateway, xfwd: true, ws: true },
      '/files': { target: gateway },
    },
  },
})
