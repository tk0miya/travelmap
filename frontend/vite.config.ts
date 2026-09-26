import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// go:embed (internal/httpapi/frontend.go) reaches only inside its own
// package directory, so the build has to land there directly rather than in
// this directory's own default dist/.
const embedOutDir = '../internal/httpapi/frontend/dist'

// The dev server serves the frontend's own module graph itself; a route Go
// still owns outright is proxied to `go run ./cmd/travelmap serve` instead
// of 404ing. Converting a page to React removes its own entry here — /,
// /login, /signup and /settings are gone already; /settings/foursquare/connect
// stays, since that one route is a browser redirect Go still serves, not a
// page.
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
      '/travelmap': backend,
      '/webhooks': backend,
      '/settings/foursquare/connect': backend,
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/setupTests.ts'],
  },
})
