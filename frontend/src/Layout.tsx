import type { ReactNode } from 'react'
import { useAuth } from './auth.tsx'

interface LayoutProps {
  children: ReactNode
}

// Layout is the header and main wrapper every page renders inside, matching
// base.html's own markup and classes exactly so style.css needs no changes.
// The header's one signed-in-only link is absent while the auth state is still
// loading: a link shown and then taken away again reads as a bug, where one
// that appears a moment late reads as the page finishing loading.
function Layout({ children }: LayoutProps) {
  const auth = useAuth()

  return (
    <>
      <header className="site-header">
        <a href="/" className="brand">
          travelmap
        </a>
        {auth.status === 'signedIn' && (
          <a href="/settings" className="header-link">
            Settings
          </a>
        )}
      </header>
      <main>{children}</main>
    </>
  )
}

export default Layout
