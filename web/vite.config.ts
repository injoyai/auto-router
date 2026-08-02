import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/v1': 'http://localhost:9090',
      '/admin': 'http://localhost:9090',
      '/health': 'http://localhost:9090',
    },
  },
})
