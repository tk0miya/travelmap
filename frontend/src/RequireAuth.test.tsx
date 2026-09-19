import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import AuthProvider from './auth.tsx'
import RequireAuth from './RequireAuth.tsx'

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

// LoginProbe stands in for LoginPage, showing the query string RequireAuth
// redirected with rather than the real form.
function LoginProbe() {
  const location = useLocation()

  return <p>login page{location.search}</p>
}

function renderProtected(initialPath: string) {
  render(
    <AuthProvider>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route
            path="/protected"
            element={
              <RequireAuth>
                <p>secret</p>
              </RequireAuth>
            }
          />
          <Route path="/login" element={<LoginProbe />} />
        </Routes>
      </MemoryRouter>
    </AuthProvider>,
  )
}

test('renders nothing while auth is still loading', () => {
  vi.mocked(fetch).mockReturnValue(new Promise(() => {}))

  renderProtected('/protected')

  expect(screen.queryByText('secret')).not.toBeInTheDocument()
  expect(screen.queryByText(/login page/)).not.toBeInTheDocument()
})

test('renders its children once signed in', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(JSON.stringify({ user: { email: 'alice@example.com' } })),
  )

  renderProtected('/protected')

  expect(await screen.findByText('secret')).toBeInTheDocument()
})

test('redirects to /login with next carrying the requested path when signed out', async () => {
  vi.mocked(fetch).mockResolvedValue(new Response('', { status: 401 }))

  renderProtected('/protected')

  expect(
    await screen.findByText('login page?next=%2Fprotected'),
  ).toBeInTheDocument()
  expect(screen.queryByText('secret')).not.toBeInTheDocument()
})
