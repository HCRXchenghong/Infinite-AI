import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 1001,
    proxy: {
      '/auth': 'http://127.0.0.1:1002',
      '/chat': 'http://127.0.0.1:1002',
      '/developer': 'http://127.0.0.1:1002',
      '/billing': 'http://127.0.0.1:1002',
      '/redeem': 'http://127.0.0.1:1002',
      '/downloads': 'http://127.0.0.1:1002',
      '/user': 'http://127.0.0.1:1002',
      '/admin': 'http://127.0.0.1:1002',
      '/v1': 'http://127.0.0.1:1002',
      '/webhooks': 'http://127.0.0.1:1002',
    },
  },
})
