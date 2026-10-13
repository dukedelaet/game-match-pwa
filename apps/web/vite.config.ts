import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { VitePWA } from 'vite-plugin-pwa'

export default defineConfig({
  plugins: [
    tailwindcss(),
    react(),
    VitePWA({
      // autoUpdate emitted skipWaiting() + clientsClaim(), so a deploy swapped
      // the worker under a live game: open tabs claimed the new worker while
      // still running the old JS, with no prompt. For a turn-based game with
      // answers in flight that is a mid-round state mismatch, so the update is
      // held until the user is told about it.
      registerType: 'prompt',
      manifest: {
        name: 'GameMatch',
        short_name: 'GameMatch',
        start_url: '/boot',
        display: 'standalone',
        background_color: '#0b0614',
        theme_color: '#0b0614',
        // A single SVG with purpose 'any' is not enough to install reliably:
        // Android wants 192 and 512 PNGs, maskable for adaptive icons, and iOS
        // uses apple-touch-icon to avoid a screenshot on Add to Home Screen.
        icons: [
          { src: '/favicon.svg', sizes: 'any', type: 'image/svg+xml', purpose: 'any' },
          { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png', purpose: 'any' },
          { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'any' },
          { src: '/icons/icon-maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
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
