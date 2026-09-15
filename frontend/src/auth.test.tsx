import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import AuthProvider, { useAuth } from './auth.tsx'

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

// Probe renders the state itself, since the state is what every consumer
// branches on and the header's link is only one of its readings.
function Probe() {
  const auth = useAuth()

  return (
    <p>
      {auth.status === 'signedIn' ? `signed in as ${auth.email}` : auth.status}
    </p>
  )
}

function renderProbe() {
  render(
    <AuthProvider>
      <Probe />
    </AuthProvider>,
  )
}

test('reports loading until users/me answers', () => {
  vi.mocked(fetch).mockReturnValue(new Promise(() => {}))

  renderProbe()

  expect(screen.getByText('loading')).toBeInTheDocument()
})

test('reports the address users/me names', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(JSON.stringify({ user: { email: 'alice@example.com' } })),
  )

  renderProbe()

  expect(
    await screen.findByText('signed in as alice@example.com'),
  ).toBeInTheDocument()
  expect(fetch).toHaveBeenCalledWith('/api/v1/users/me')
})

// The empty body is what requireUser actually answers a browser with no
// session, so parsing it has to fail harmlessly rather than throw.
test('reports signed out when users/me refuses the request', async () => {
  vi.mocked(fetch).mockResolvedValue(new Response('', { status: 401 }))

  renderProbe()

  expect(await screen.findByText('signedOut')).toBeInTheDocument()
})

test('reports signed out when the request itself fails', async () => {
  vi.mocked(fetch).mockRejectedValue(new Error('offline'))

  renderProbe()

  expect(await screen.findByText('signedOut')).toBeInTheDocument()
})

test('refuses to be read outside a provider', () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})

  expect(() => render(<Probe />)).toThrow(
    'useAuth must be called inside an AuthProvider',
  )
})
