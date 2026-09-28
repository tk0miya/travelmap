import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import App from './App.tsx'

// The shell asks who the browser is as soon as it mounts; these tests are
// about routing, so every one of them answers nobody.
beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(new Response('', { status: 401 })),
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

test('renders the placeholder shell for a path with no page yet', () => {
  window.history.pushState({}, '', '/not-a-real-page')

  render(<App />)

  expect(screen.getByText('Coming soon')).toBeInTheDocument()
})

test('renders the home page at / when signed in', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(JSON.stringify({ user: { email: 'alice@example.com' } })),
  )

  render(<App />)

  expect(
    await screen.findByText('Signed in as alice@example.com.'),
  ).toBeInTheDocument()
})

test('redirects a signed-out visit to / to the login form', async () => {
  render(<App />)

  expect(
    await screen.findByRole('heading', { name: 'Log in' }),
  ).toBeInTheDocument()
  expect(window.location.pathname).toBe('/login')
})

test('renders the settings page at /settings when signed in', async () => {
  vi.mocked(fetch).mockImplementation((input) => {
    if (input === '/travelmap/web/foursquare_account') {
      return Promise.resolve(new Response('', { status: 404 }))
    }

    return Promise.resolve(
      new Response(JSON.stringify({ user: { email: 'alice@example.com' } })),
    )
  })
  window.history.pushState({}, '', '/settings')

  render(<App />)

  expect(
    await screen.findByRole('heading', { name: 'Settings' }),
  ).toBeInTheDocument()
})

test('redirects a signed-out visit to /settings to the login form', async () => {
  window.history.pushState({}, '', '/settings')

  render(<App />)

  expect(
    await screen.findByRole('heading', { name: 'Log in' }),
  ).toBeInTheDocument()
  expect(window.location.pathname).toBe('/login')
  expect(window.location.search).toBe('?next=%2Fsettings')
})

test('renders the login page at /login', () => {
  window.history.pushState({}, '', '/login')

  render(<App />)

  expect(screen.getByRole('heading', { name: 'Log in' })).toBeInTheDocument()
})

test('renders the signup page at /signup', () => {
  window.history.pushState({}, '', '/signup')

  render(<App />)

  expect(screen.getByRole('heading', { name: 'Sign up' })).toBeInTheDocument()
})

// What separates a Link from a plain anchor here is that the path moves at
// all: jsdom navigates for neither, so an anchor would leave the location on
// /login and render nothing new.
test('swaps the page in place when a link between two of them is followed', async () => {
  window.history.pushState({}, '', '/login')

  render(<App />)
  fireEvent.click(screen.getByRole('link', { name: 'Sign up' }))

  expect(
    await screen.findByRole('heading', { name: 'Sign up' }),
  ).toBeInTheDocument()
  expect(
    screen.queryByRole('heading', { name: 'Log in' }),
  ).not.toBeInTheDocument()
  expect(window.location.pathname).toBe('/signup')
})

// The other half of that rule, which otherwise fails silently: a path with no
// Route is Go's to serve, so its link has to stay an anchor —
// SettingsPage's own link starting the Swarm OAuth flow, which answers a
// browser navigation rather than a JSON action. jsdom navigates for neither
// kind, so the path standing still is what says this one is an anchor — as a
// Link it would move and render the no-Route placeholder.
test('leaves the app for a link to a path it has no route for', async () => {
  vi.mocked(fetch).mockImplementation((input) => {
    if (input === '/travelmap/web/foursquare_account') {
      return Promise.resolve(new Response('', { status: 404 }))
    }

    return Promise.resolve(
      new Response(JSON.stringify({ user: { email: 'alice@example.com' } })),
    )
  })
  window.history.pushState({}, '', '/settings')

  render(<App />)
  fireEvent.click(
    await screen.findByRole('link', { name: 'Connect your Swarm account' }),
  )

  expect(window.location.pathname).toBe('/settings')
  expect(screen.queryByText('Coming soon')).not.toBeInTheDocument()
})
