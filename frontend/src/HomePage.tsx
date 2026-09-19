import { useAuth } from './auth.tsx'

// HomePage is what templates/index.html rendered server-side: the signed-in
// address, and a way to sign out. Matches that old page's markup and
// classes, the same as LoginPage and SignupPage already do for theirs.
// RequireAuth guarantees auth.status is 'signedIn' by the time this renders,
// since every route reaching it is wrapped in one.
function HomePage() {
  const auth = useAuth()

  if (auth.status !== 'signedIn') {
    return null
  }

  const handleLogout = async () => {
    await fetch('/travelmap/web/session', { method: 'DELETE' })
    window.location.href = '/login'
  }

  return (
    <div className="status">
      <p>Signed in as {auth.email}.</p>
      <button type="button" className="link-button" onClick={handleLogout}>
        Log out
      </button>
    </div>
  )
}

export default HomePage
