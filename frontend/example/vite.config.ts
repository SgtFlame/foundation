import react from '@vitejs/plugin-react'
import { foundation } from '@foundation/common/vite'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), foundation()],
})
