import { type FormEvent, useState } from 'react'

// nextPath reads the `next` query parameter a redirect to /login carries
// (see requireSessionUser) and returns it only if it resolves to travelmap's
// own origin — resolving it, rather than pattern-matching its prefix, is
// what a leading `//` and a leading `/\` have in common with a next hidden
// behind a tab or newline the URL parser strips before resolving: all of
// them turn into a scheme-relative reference to another host once actually
// navigated to, and only resolving `next` the same way a navigation would
// catches all of them at once. Falls back to / otherwise, including when
// there is no `next` at all.
function nextPath(): string {
  const next = new URLSearchParams(window.location.search).get('next')
  if (!next?.startsWith('/')) {
    return '/'
  }

  try {
    return new URL(next, window.location.origin).origin ===
      window.location.origin
      ? next
      : '/'
  } catch {
    return '/'
  }
}

// LoginPage is the sign-in form, matching the old login.html's markup and
// classes. A successful POST /travelmap/web/session sets the cookie itself;
// this only has to send the browser on afterwards — to `next` if the
// redirect here carried one, back to / otherwise — which is still a full
// navigation since neither page is React yet.
function LoginPage() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    setError('')

    const res = await fetch('/travelmap/web/session', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password }),
    })

    if (res.ok) {
      window.location.href = nextPath()
      return
    }

    const body: unknown = await res.json().catch(() => null)
    const message =
      body &&
      typeof body === 'object' &&
      'error' in body &&
      typeof body.error === 'string'
        ? body.error
        : 'Something went wrong'

    setError(message)
  }

  return (
    <div className="card">
      <h1>Log in</h1>
      {error && <p className="error">{error}</p>}
      <form onSubmit={handleSubmit}>
        <div className="field">
          <label htmlFor="email">Email</label>
          <input
            id="email"
            type="email"
            name="email"
            autoComplete="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor="password">Password</label>
          <input
            id="password"
            type="password"
            name="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        <button type="submit">Log in</button>
      </form>
      <p className="auth-links">
        Don't have an account? <a href="/signup">Sign up</a>
      </p>
    </div>
  )
}

export default LoginPage
