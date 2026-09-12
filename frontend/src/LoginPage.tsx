import { type FormEvent, useState } from 'react'

// LoginPage is the sign-in form, matching the old login.html's markup and
// classes. A successful POST /api/session sets the cookie itself; this only
// has to send the browser on to / afterwards, which is still a full
// navigation since that page is not React yet.
function LoginPage() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    setError('')

    const res = await fetch('/api/session', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password }),
    })

    if (res.ok) {
      window.location.href = '/'
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
