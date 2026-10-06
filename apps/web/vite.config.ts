import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { VitePWA } from 'vite-plugin-pwa'

export default defineConfig({
  plugins: [
    tailwindcss(),
    react(),
    VitePWA({
      registerType: 'autoUpdate',
      manifest: {
        name: 'GameMatch',
        short_name: 'GameMatch',
        start_url: '/boot',
        display: 'standalone',
        background_color: '#0b0614',
        theme_color: '#0b0614',
        icons: [{ src: '/favicon.svg', sizes: 'any', type: 'image/svg+xml', purpose: 'any' }],
      },
      workbox: {
        navigateFallback: '/offline',
        globPatterns: ['**/*.{js,css,html,svg,ico,webp}'],
        // Registers the Web Push listeners alongside the generated worker.
        importScripts: ['push-sw.js'],
      },
    }),
  ],
  server: {
    host: '0.0.0.0',
    port: 5173,
    allowedHosts: ['limitlessmentoring.cloud', '.limitlessmentoring.cloud'],
    proxy: {
      '/v1': 'http://127.0.0.1:8000',
    },
  },
})
