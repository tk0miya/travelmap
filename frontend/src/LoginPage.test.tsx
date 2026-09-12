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
    '/api/session',
    expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ email: 'alice@example.com', password: 'secret' }),
    }),
  )
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
