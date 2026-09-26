import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import SettingsPage from './SettingsPage.tsx'

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderSettingsPage() {
  render(<SettingsPage />, { wrapper: MemoryRouter })
}

test('shows no Swarm account is connected yet', async () => {
  vi.mocked(fetch).mockResolvedValue(new Response('', { status: 404 }))

  renderSettingsPage()

  expect(
    await screen.findByText('No Swarm account is connected yet.'),
  ).toBeInTheDocument()
  expect(
    screen.getByRole('link', { name: 'Connect your Swarm account' }),
  ).toHaveAttribute('href', '/settings/foursquare/connect')
})

test('shows the linked account with nothing fetched yet', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(
      JSON.stringify({ foursquare_user_id: '1709193', synced_through: null }),
    ),
  )

  renderSettingsPage()

  expect(
    await screen.findByText('Connected as Swarm user 1709193.'),
  ).toBeInTheDocument()
  expect(screen.getByText('No check-ins fetched yet.')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Disconnect' })).toBeInTheDocument()
})

test('reports when check-ins were last fetched', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(
      JSON.stringify({
        foursquare_user_id: '1709193',
        synced_through: '2026-03-04T05:06:07.000Z',
      }),
    ),
  )

  renderSettingsPage()

  expect(
    await screen.findByText(/Check-ins fetched through/),
  ).toBeInTheDocument()
  expect(
    screen.queryByText('No check-ins fetched yet.'),
  ).not.toBeInTheDocument()
})

test('disconnects through DELETE /travelmap/web/foursquare_account', async () => {
  vi.mocked(fetch).mockImplementation((_input, init) => {
    if (init?.method === 'DELETE') {
      return Promise.resolve(new Response(null, { status: 204 }))
    }

    return Promise.resolve(
      new Response(
        JSON.stringify({ foursquare_user_id: '1709193', synced_through: null }),
      ),
    )
  })

  renderSettingsPage()
  fireEvent.click(await screen.findByRole('button', { name: 'Disconnect' }))

  expect(
    await screen.findByText('No Swarm account is connected yet.'),
  ).toBeInTheDocument()
  expect(fetch).toHaveBeenCalledWith('/travelmap/web/foursquare_account', {
    method: 'DELETE',
  })
})

test('shows an error when the account cannot be loaded', async () => {
  vi.mocked(fetch).mockResolvedValue(new Response('', { status: 500 }))

  renderSettingsPage()

  expect(
    await screen.findByText(
      'Something went wrong loading your Swarm connection.',
    ),
  ).toBeInTheDocument()
})

test('links back to the home page', () => {
  vi.mocked(fetch).mockReturnValue(new Promise(() => {}))

  renderSettingsPage()

  expect(screen.getByRole('link', { name: 'Back' })).toHaveAttribute(
    'href',
    '/',
  )
})
