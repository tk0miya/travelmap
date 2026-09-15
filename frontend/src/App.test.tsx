import { fireEvent, render, screen } from '@testing-library/react'
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
// Route is Go's to serve, so its link has to stay an anchor. jsdom navigates
// for neither kind, so the path standing still is what says this one is an
// anchor — as a Link it would move and render the no-Route placeholder.
test('leaves the app for a link to a path it has no route for', () => {
  window.history.pushState({}, '', '/login')

  render(<App />)
  fireEvent.click(screen.getByRole('link', { name: 'travelmap' }))

  expect(window.location.pathname).toBe('/login')
  expect(screen.queryByText('Coming soon')).not.toBeInTheDocument()
})
