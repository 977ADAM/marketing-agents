/// <reference types="vitest" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  // Базовый путь сборки. По умолчанию '/' (standalone), под interpool
  // приложение монтируется на /marketing/ — задаётся build-arg PUBLIC_BASE.
  base: process.env.PUBLIC_BASE || '/',
  plugins: [react()],
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: { '/api': 'http://localhost:8080' },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
  },
})
