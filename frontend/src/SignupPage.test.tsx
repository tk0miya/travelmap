import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import SignupPage from './SignupPage.tsx'

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function fillForm(email: string, password: string, confirmation: string) {
  fireEvent.change(screen.getByLabelText('Email'), { target: { value: email } })
  fireEvent.change(screen.getByLabelText('Password'), {
    target: { value: password },
  })
  fireEvent.change(screen.getByLabelText('Confirm password'), {
    target: { value: confirmation },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Sign up' }))
}

test('renders the form', () => {
  render(<SignupPage />)

  expect(screen.getByLabelText('Email')).toBeInTheDocument()
  expect(screen.getByLabelText('Password')).toBeInTheDocument()
  expect(screen.getByLabelText('Confirm password')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Sign up' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Log in' })).toHaveAttribute(
    'href',
    '/login',
  )
})

test('shows the API key on a successful sign-up', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(JSON.stringify({ api_key: 'abc123' }), { status: 201 }),
  )

  render(<SignupPage />)
  fillForm(
    'alice@example.com',
    'correct horse battery',
    'correct horse battery',
  )

  expect(await screen.findByText('Account created')).toBeInTheDocument()
  expect(screen.getByText('abc123')).toBeInTheDocument()

  expect(fetch).toHaveBeenCalledWith(
    '/travelmap/web/users',
    expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({
        email: 'alice@example.com',
        password: 'correct horse battery',
        password_confirmation: 'correct horse battery',
      }),
    }),
  )
})

test('shows the duplicate-email error under the email field', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(JSON.stringify({ email_error: 'already registered' }), {
      status: 422,
    }),
  )

  render(<SignupPage />)
  fillForm(
    'alice@example.com',
    'correct horse battery',
    'correct horse battery',
  )

  expect(await screen.findByText('already registered')).toBeInTheDocument()
  expect(screen.queryByText('Account created')).not.toBeInTheDocument()
})

test('shows the too-short-password error under the password field', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(
      JSON.stringify({ password_error: 'must be at least 8 bytes' }),
      { status: 422 },
    ),
  )

  render(<SignupPage />)
  fillForm('alice@example.com', 'short', 'short')

  expect(
    await screen.findByText('must be at least 8 bytes'),
  ).toBeInTheDocument()
})

test('shows the mismatched-confirmation error under the confirmation field', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(
      JSON.stringify({ confirm_error: 'does not match the password' }),
      { status: 422 },
    ),
  )

  render(<SignupPage />)
  fillForm('alice@example.com', 'correct horse battery', 'something else')

  expect(
    await screen.findByText('does not match the password'),
  ).toBeInTheDocument()
})

test('falls back to a generic message for a refusal naming none of the three fields', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(JSON.stringify({ error: 'internal server error' }), {
      status: 500,
    }),
  )

  render(<SignupPage />)
  fillForm(
    'alice@example.com',
    'correct horse battery',
    'correct horse battery',
  )

  expect(await screen.findByText('internal server error')).toBeInTheDocument()
})
