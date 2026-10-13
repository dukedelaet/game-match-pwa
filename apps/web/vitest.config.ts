import { defineConfig } from 'vitest/config'

// Kept separate from vite.config.ts so the test block does not need the Vitest
// types in the production build's config, and so tsc -b does not type-check
// test files under the app's compiler options.
export default defineConfig({
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
