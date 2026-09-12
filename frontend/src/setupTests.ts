import { cleanup } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'

// vitest's own globals are off (see vite.config.ts), so React Testing
// Library's usual auto-cleanup-on-afterEach never triggers on its own: it
// only looks for a global afterEach, and importing one into a test file's
// module scope does not create one.
afterEach(cleanup)
