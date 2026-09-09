import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// go:embed (internal/httpapi/frontend.go) reaches only inside its own
// package directory, so the build has to land there directly rather than in
// this directory's own default dist/.
const embedOutDir = '../internal/httpapi/frontend/dist'

// The dev server serves the frontend's own module graph itself; a page
// still built from html/template is proxied to
// `go run ./cmd/travelmap serve` instead of 404ing. Converting a page to
// React removes its own entry here — /login is gone already.
const backend = 'http://localhost:3000'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: embedOutDir,
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': backend,
      '/webhooks': backend,
      '/signup': backend,
      '/settings': backend,
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/setupTests.ts'],
  },
})
