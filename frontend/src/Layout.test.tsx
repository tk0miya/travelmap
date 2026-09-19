import { act, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import AuthProvider from './auth.tsx'
import Layout from './Layout.tsx'

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderLayout() {
  render(
    <AuthProvider>
      <MemoryRouter>
        <Layout>
          <p>content</p>
        </Layout>
      </MemoryRouter>
    </AuthProvider>,
  )
}

test('renders the brand link and its children', () => {
  vi.mocked(fetch).mockResolvedValue(new Response('', { status: 401 }))

  renderLayout()

  expect(screen.getByRole('link', { name: 'travelmap' })).toHaveAttribute(
    'href',
    '/',
  )
  expect(screen.getByText('content')).toBeInTheDocument()
})

test('shows the Settings link once the browser is known to be signed in', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(JSON.stringify({ user: { email: 'alice@example.com' } })),
  )

  renderLayout()

  expect(await screen.findByRole('link', { name: 'Settings' })).toHaveAttribute(
    'href',
    '/settings',
  )
})

// An absent link is also what a header renders before any answer arrives, so
// this waits on the answer itself rather than on anything it can query: the
// state has to have settled for the absence to mean anything.
test('shows no Settings link to a signed-out browser', async () => {
  const answered = Promise.resolve(new Response('', { status: 401 }))
  vi.mocked(fetch).mockReturnValue(answered)

  renderLayout()
  await act(async () => {
    await answered
  })

  expect(
    screen.queryByRole('link', { name: 'Settings' }),
  ).not.toBeInTheDocument()
})
