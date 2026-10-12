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
        // Must be a precached build output. Workbox binds the fallback against
        // the precache manifest while the worker is being evaluated and throws
        // on a miss, so naming a client-side route here stops the worker from
        // installing at all. The router renders /offline from the shell.
        navigateFallback: '/index.html',
        globPatterns: ['**/*.{js,css,html,svg,ico,webp}'],
        // Registers the Web Push listeners alongside the generated worker.
        importScripts: ['push-sw.js'],
      },
      // Surface worker-generation bugs in dev, where the worker otherwise never loads.
      devOptions: { enabled: true },
    }),
  ],
  server: {
    host: '0.0.0.0',
    port: 5173,
    allowedHosts: [
      'limitlessmentoring.cloud',
      '.limitlessmentoring.cloud',
      // Reachable over the tailnet from phones and other devices.
      'duke-optiplex',
      'duke-optiplex.tailf93cd8.ts.net',
      '.ts.net',
    ],
    proxy: {
      '/v1': 'http://127.0.0.1:8000',
    },
  },
})
