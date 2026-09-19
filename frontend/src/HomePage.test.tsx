import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import AuthProvider from './auth.tsx'
import HomePage from './HomePage.tsx'

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
  Object.defineProperty(window, 'location', {
    writable: true,
    value: { href: '' },
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderHomePage() {
  render(
    <AuthProvider>
      <HomePage />
    </AuthProvider>,
  )
}

test('names the signed-in address', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(JSON.stringify({ user: { email: 'alice@example.com' } })),
  )

  renderHomePage()

  expect(
    await screen.findByText('Signed in as alice@example.com.'),
  ).toBeInTheDocument()
})

test('logs out through DELETE /travelmap/web/session', async () => {
  vi.mocked(fetch).mockImplementation((input) => {
    if (input === '/travelmap/web/session') {
      return Promise.resolve(new Response(null, { status: 204 }))
    }

    return Promise.resolve(
      new Response(JSON.stringify({ user: { email: 'alice@example.com' } })),
    )
  })

  renderHomePage()
  fireEvent.click(await screen.findByRole('button', { name: 'Log out' }))

  await vi.waitFor(() =>
    expect(fetch).toHaveBeenCalledWith('/travelmap/web/session', {
      method: 'DELETE',
    }),
  )
  await vi.waitFor(() => expect(window.location.href).toBe('/login'))
})
