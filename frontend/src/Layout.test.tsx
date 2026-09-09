import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'
import Layout from './Layout.tsx'

test('renders the brand link, its children, and no Settings link while hard-coded signed out', () => {
  render(
    <Layout>
      <p>content</p>
    </Layout>,
  )

  expect(screen.getByRole('link', { name: 'travelmap' })).toHaveAttribute(
    'href',
    '/',
  )
  expect(
    screen.queryByRole('link', { name: 'Settings' }),
  ).not.toBeInTheDocument()
  expect(screen.getByText('content')).toBeInTheDocument()
})
