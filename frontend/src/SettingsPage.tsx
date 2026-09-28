import { useEffect, useState } from 'react'
import { Link } from 'react-router'

// AccountState is what GET /travelmap/web/foursquare_account answers, folded
// into one type this page renders from. It starts out loading rather than
// unlinked, the same reasoning as AuthState: nothing was embedded in the
// static shell, so showing "not connected" before the fetch answers would
// flash the wrong state for however long the round trip takes.
type AccountState =
  | { status: 'loading' }
  | { status: 'linked'; foursquareUserId: string; syncedThrough: string | null }
  | { status: 'unlinked' }
  | { status: 'error' }

// SettingsPage is the Swarm connection screen, matching the old
// settings.html's markup and classes. Foursquare is its only section today.
function SettingsPage() {
  const [state, setState] = useState<AccountState>({ status: 'loading' })

  useEffect(() => {
    let current = true

    fetchAccount().then((next) => {
      if (current) {
        setState(next)
      }
    })

    return () => {
      current = false
    }
  }, [])

  const handleDisconnect = async () => {
    const res = await fetch('/travelmap/web/foursquare_account', {
      method: 'DELETE',
    })

    if (res.ok) {
      setState({ status: 'unlinked' })
    }
  }

  return (
    <div className="card">
      <h1>Settings</h1>
      <h2>Swarm connection</h2>
      {state.status === 'linked' && (
        <>
          <p>Connected as Swarm user {state.foursquareUserId}.</p>
          <p>
            {state.syncedThrough
              ? `Check-ins fetched through ${new Date(state.syncedThrough).toLocaleString()}.`
              : 'No check-ins fetched yet.'}
          </p>
          <button type="button" onClick={handleDisconnect}>
            Disconnect
          </button>
          <p className="auth-links">
            Disconnecting keeps the check-ins already collected — it only
            removes the link, so a different Swarm account can be connected
            instead.
          </p>
        </>
      )}
      {state.status === 'unlinked' && (
        <>
          <p>No Swarm account is connected yet.</p>
          <p>
            <a href="/settings/foursquare/connect">
              Connect your Swarm account
            </a>
          </p>
        </>
      )}
      {state.status === 'error' && (
        <p className="error">
          Something went wrong loading your Swarm connection.
        </p>
      )}
      <p className="auth-links">
        <Link to="/">Back</Link>
      </p>
    </div>
  )
}

// isFoursquareAccountBody narrows an unknown JSON body to what
// dto.FoursquareAccountResponse actually sends, the same defensive shape
// auth.tsx's own signedInEmail checks a fetch response against.
function isFoursquareAccountBody(
  body: unknown,
): body is { foursquare_user_id: string; synced_through: string | null } {
  return (
    !!body &&
    typeof body === 'object' &&
    'foursquare_user_id' in body &&
    typeof body.foursquare_user_id === 'string' &&
    'synced_through' in body &&
    (body.synced_through === null || typeof body.synced_through === 'string')
  )
}

// fetchAccount asks GET /travelmap/web/foursquare_account and turns its
// answer into an AccountState: 404 is "unlinked", not an error — it is the
// resource's own way of saying so — and everything else that is not a clean
// 200 (a network failure, a 500, a body this page cannot parse) collapses
// into "error" rather than being told apart, since this page has nothing
// different to do for any of them.
async function fetchAccount(): Promise<AccountState> {
  const res = await fetch('/travelmap/web/foursquare_account').catch(() => null)
  if (!res) {
    return { status: 'error' }
  }

  if (res.status === 404) {
    return { status: 'unlinked' }
  }

  if (!res.ok) {
    return { status: 'error' }
  }

  const body: unknown = await res.json().catch(() => null)
  if (!isFoursquareAccountBody(body)) {
    return { status: 'error' }
  }

  return {
    status: 'linked',
    foursquareUserId: body.foursquare_user_id,
    syncedThrough: body.synced_through,
  }
}

export default SettingsPage
