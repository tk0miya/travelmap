import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import LoginPage from './LoginPage.tsx'

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

test('renders the form', () => {
  render(<LoginPage />)

  expect(screen.getByLabelText('Email')).toBeInTheDocument()
  expect(screen.getByLabelText('Password')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Log in' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Sign up' })).toHaveAttribute(
    'href',
    '/signup',
  )
})

test('redirects to / on a successful sign-in', async () => {
  vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 201 }))

  render(<LoginPage />)

  fireEvent.change(screen.getByLabelText('Email'), {
    target: { value: 'alice@example.com' },
  })
  fireEvent.change(screen.getByLabelText('Password'), {
    target: { value: 'secret' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Log in' }))

  await vi.waitFor(() => expect(window.location.href).toBe('/'))

  expect(fetch).toHaveBeenCalledWith(
    '/travelmap/web/session',
    expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ email: 'alice@example.com', password: 'secret' }),
    }),
  )
})

test('redirects to next after a successful sign-in when the redirect here carried one', async () => {
  vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 201 }))
  Object.defineProperty(window, 'location', {
    writable: true,
    value: {
      href: '',
      origin: 'https://travelmap.example',
      search: '?next=%2Fsettings',
    },
  })

  render(<LoginPage />)

  fireEvent.change(screen.getByLabelText('Email'), {
    target: { value: 'alice@example.com' },
  })
  fireEvent.change(screen.getByLabelText('Password'), {
    target: { value: 'secret' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Log in' }))

  await vi.waitFor(() => expect(window.location.href).toBe('/settings'))
})

test('falls back to / when next does not point to a same-origin path', async () => {
  vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 201 }))
  Object.defineProperty(window, 'location', {
    writable: true,
    value: {
      href: '',
      origin: 'https://travelmap.example',
      search: '?next=%2F%2Fevil.example',
    },
  })

  render(<LoginPage />)

  fireEvent.change(screen.getByLabelText('Email'), {
    target: { value: 'alice@example.com' },
  })
  fireEvent.change(screen.getByLabelText('Password'), {
    target: { value: 'secret' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Log in' }))

  await vi.waitFor(() => expect(window.location.href).toBe('/'))
})

// A tab between the leading slashes is not itself `//`, but the URL parser
// strips ASCII tab/newline/CR before resolving a reference, so a prefix
// check alone would have let this one through as `//evil.example` once
// navigated to.
test('falls back to / when next hides a scheme-relative reference behind a stripped tab', async () => {
  vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 201 }))
  Object.defineProperty(window, 'location', {
    writable: true,
    value: {
      href: '',
      origin: 'https://travelmap.example',
      search: '?next=%2F%09%2Fevil.example',
    },
  })

  render(<LoginPage />)

  fireEvent.change(screen.getByLabelText('Email'), {
    target: { value: 'alice@example.com' },
  })
  fireEvent.change(screen.getByLabelText('Password'), {
    target: { value: 'secret' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Log in' }))

  await vi.waitFor(() => expect(window.location.href).toBe('/'))
})

test('shows the error message on a refused sign-in', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(JSON.stringify({ error: 'Invalid email or password' }), {
      status: 401,
    }),
  )

  render(<LoginPage />)

  fireEvent.change(screen.getByLabelText('Email'), {
    target: { value: 'alice@example.com' },
  })
  fireEvent.change(screen.getByLabelText('Password'), {
    target: { value: 'wrong' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Log in' }))

  expect(
    await screen.findByText('Invalid email or password'),
  ).toBeInTheDocument()
})
