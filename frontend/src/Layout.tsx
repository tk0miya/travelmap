import type { ReactNode } from 'react'

interface LayoutProps {
  children: ReactNode
}

// Layout is the header and main wrapper every page renders inside, matching
// base.html's own markup and classes exactly so style.css needs no changes.
// SignedIn is hard-coded false: nothing here fetches the browser's actual
// auth state yet.
function Layout({ children }: LayoutProps) {
  const signedIn = false

  return (
    <>
      <header className="site-header">
        <a href="/" className="brand">
          travelmap
        </a>
        {signedIn && (
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
