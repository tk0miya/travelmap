import { render, screen } from '@testing-library/react'
import { afterEach, expect, test } from 'vitest'
import App from './App.tsx'

afterEach(() => {
  window.history.pushState({}, '', '/')
})

test('renders the placeholder shell for a path with no page yet', () => {
  render(<App />)

  expect(screen.getByText('Coming soon')).toBeInTheDocument()
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
