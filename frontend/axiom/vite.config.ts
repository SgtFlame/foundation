import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  // y-monaco and the editor must share one Monaco and one Yjs
  resolve: {
    dedupe: ['monaco-editor', 'yjs'],
    alias: [
      {
        // y-monaco imports this path, which monaco-editor 0.57's exports map no longer allows
        find: /^monaco-editor\/esm\/vs\/editor\/editor\.api\.js$/,
        replacement: fileURLToPath(
          new URL('./node_modules/monaco-editor/esm/vs/editor/editor.api.js', import.meta.url),
        ),
      },
    ],
  },
  server: {
    proxy: {
      '/api/v1/kb': { target: 'http://localhost:8080', ws: true },
    },
  },
})
