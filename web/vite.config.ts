import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// Build straight into the Go embed location, so `make web` + `go build` produces one binary with the
// interface inside. emptyOutDir is explicit because the target is outside the web/ root.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 900,
    rollupOptions: {
      output: {
        // Split the heavy visualization libraries into their own chunks so the initial
        // parse is smaller and each library caches independently across releases.
        manualChunks: {
          react: ['react', 'react-dom', 'react-router-dom', 'react-redux', '@reduxjs/toolkit'],
          echarts: ['echarts'],
          charts: ['lightweight-charts'],
          flow: ['@xyflow/react'],
          editor: ['codemirror', '@codemirror/view', '@codemirror/state', '@codemirror/theme-one-dark'],
          table: ['@tanstack/react-table'],
        },
      },
    },
  },
})
