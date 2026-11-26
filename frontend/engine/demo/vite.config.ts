import { defineConfig } from 'vite'

export default defineConfig({
  worker: { format: 'es' },
  build: { target: 'es2023', outDir: '../dist' },
})
