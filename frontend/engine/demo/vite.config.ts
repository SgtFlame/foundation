import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'

export default defineConfig({
  worker: { format: 'es' },
  build: { target: 'es2023', outDir: '../dist' },
  // The area demo reads the server's built-in piece set from the backend
  server: { fs: { allow: [fileURLToPath(new URL('../../..', import.meta.url))] } },
})
