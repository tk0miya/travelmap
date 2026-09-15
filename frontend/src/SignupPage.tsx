import { type FormEvent, useState } from 'react'

// stringField reads field out of a POST /travelmap/web/users response body —
// api_key on success, or one of email_error/password_error/confirm_error on
// a refusal (at most one is ever set, since validation stops at the first
// failure) — and "" if the body has nothing usable under that name.
function stringField(body: unknown, field: string): string {
  return body &&
    typeof body === 'object' &&
    field in body &&
    typeof body[field as keyof typeof body] === 'string'
    ? (body[field as keyof typeof body] as string)
    : ''
}

// SignupPage is the sign-up form, matching the old signup.html's markup and
// classes. A successful POST /travelmap/web/users sets the session cookie
// itself and returns the new account's api_key, which this then shows in
// place of the form — the only place it is ever shown again.
function SignupPage() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [emailError, setEmailError] = useState('')
  const [passwordError, setPasswordError] = useState('')
  const [confirmError, setConfirmError] = useState('')
  // error is the fallback for a refusal that names none of the three fields
  // above — a malformed request body or a server error, neither of which
  // dto.CreateUserError ever carries a field for.
  const [error, setError] = useState('')
  const [apiKey, setApiKey] = useState<string | null>(null)

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    setEmailError('')
    setPasswordError('')
    setConfirmError('')
    setError('')

    const res = await fetch('/travelmap/web/users', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        email,
        password,
        password_confirmation: confirmation,
      }),
    })

    const body: unknown = await res.json().catch(() => null)

    if (res.ok) {
      setApiKey(stringField(body, 'api_key'))
      return
    }

    const emailErr = stringField(body, 'email_error')
    const passwordErr = stringField(body, 'password_error')
    const confirmErr = stringField(body, 'confirm_error')

    setEmailError(emailErr)
    setPasswordError(passwordErr)
    setConfirmError(confirmErr)

    if (!emailErr && !passwordErr && !confirmErr) {
      setError(stringField(body, 'error') || 'Something went wrong')
    }
  }

  if (apiKey !== null) {
    return (
      <div className="card">
        <h1>Account created</h1>
        <p>Your API key, for configuring the phone app:</p>
        <p className="api-key">{apiKey}</p>
        <p>
          <a href="/">Continue</a>
        </p>
      </div>
    )
  }

  return (
    <div className="card">
      <h1>Sign up</h1>
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
          {emailError && <p className="field-error">{emailError}</p>}
        </div>
        <div className="field">
          <label htmlFor="password">Password</label>
          <input
            id="password"
            type="password"
            name="password"
            autoComplete="new-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          {passwordError && <p className="field-error">{passwordError}</p>}
        </div>
        <div className="field">
          <label htmlFor="password_confirmation">Confirm password</label>
          <input
            id="password_confirmation"
            type="password"
            name="password_confirmation"
            autoComplete="new-password"
            required
            value={confirmation}
            onChange={(e) => setConfirmation(e.target.value)}
          />
          {confirmError && <p className="field-error">{confirmError}</p>}
        </div>
        <button type="submit">Sign up</button>
      </form>
      <p className="auth-links">
        Already have an account? <a href="/login">Log in</a>
      </p>
    </div>
  )
}

export default SignupPage
