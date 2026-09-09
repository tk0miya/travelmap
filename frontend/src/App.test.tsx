import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'
import App from './App.tsx'

test('renders the placeholder shell', () => {
  render(<App />)

  expect(screen.getByText('travelmap')).toBeInTheDocument()
})
