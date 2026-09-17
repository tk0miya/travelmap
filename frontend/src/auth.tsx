import {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useState,
} from 'react'

// AuthState is what the browser knows about its own session. It starts out
// unknown rather than signed out: the shell is static, so nothing was embedded
// in it at render time and the answer only arrives once the request below
// does. A caller that treats loading as signed out sends a signed-in visitor
// to the login form for as long as the round trip takes.
export type AuthState =
  | { status: 'loading' }
  | { status: 'signedIn'; email: string }
  | { status: 'signedOut' }

const AuthContext = createContext<AuthState | undefined>(undefined)

// useAuth returns the state AuthProvider fetched. Outside a provider it
// throws rather than reporting a signed-out browser, which would render as a
// page that simply never signs anyone in.
export function useAuth(): AuthState {
  const state = useContext(AuthContext)
  if (!state) {
    throw new Error('useAuth must be called inside an AuthProvider')
  }

  return state
}

// AuthProvider asks who the session cookie names, once, and hands the answer
// to everything below it. It asks /api/v1/users/me — the API's own endpoint,
// not a browser-only one — because the cookie authenticates /api/v1, which is
// what lets the browser reuse the API a Dawarich client uses.
function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ status: 'loading' })

  useEffect(() => {
    let current = true

    signedInEmail().then((email) => {
      if (!current) {
        return
      }

      setState(email ? { status: 'signedIn', email } : { status: 'signedOut' })
    })

    return () => {
      current = false
    }
  }, [])

  return <AuthContext value={state}>{children}</AuthContext>
}

// signedInEmail returns the address /api/v1/users/me reports, or null for a
// browser it does not recognise.
//
// Everything that is not a user is that same null: a 401 is the ordinary
// signed-out answer, and a request that failed outright leaves a visitor with
// nothing to do differently from having been signed out — the pages they can
// reach are the signed-out ones either way.
async function signedInEmail(): Promise<string | null> {
  const res = await fetch('/api/v1/users/me').catch(() => null)
  if (!res?.ok) {
    return null
  }

  const body: unknown = await res.json().catch(() => null)
  if (
    body &&
    typeof body === 'object' &&
    'user' in body &&
    body.user &&
    typeof body.user === 'object' &&
    'email' in body.user &&
    typeof body.user.email === 'string'
  ) {
    return body.user.email
  }

  return null
}

export default AuthProvider
