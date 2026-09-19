import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router'
import { useAuth } from './auth.tsx'

// RequireAuth is the client-side counterpart to the server's own
// requireSessionUser: a signed-out visitor is sent to /login with `next`
// carrying the path they asked for, the same way, so a successful sign-in
// lands them back here instead of always at /. Nothing renders while the
// auth state is still loading — see AuthState for why that is not the same
// as signed out.
function RequireAuth({ children }: { children: ReactNode }) {
  const auth = useAuth()
  const location = useLocation()

  if (auth.status === 'loading') {
    return null
  }

  if (auth.status === 'signedOut') {
    const next = `${location.pathname}${location.search}`
    return (
      <Navigate
        to={`/login?${new URLSearchParams({ next }).toString()}`}
        replace
      />
    )
  }

  return children
}

export default RequireAuth
