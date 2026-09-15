import Layout from './Layout.tsx'
import LoginPage from './LoginPage.tsx'
import SignupPage from './SignupPage.tsx'

// App picks a page by the browser's current path. There is no router yet:
// the server only serves this shell for a path no explicit route claims,
// and /login and /signup are the pages converted so far.
function App() {
  if (window.location.pathname === '/login') {
    return (
      <Layout>
        <LoginPage />
      </Layout>
    )
  }

  if (window.location.pathname === '/signup') {
    return (
      <Layout>
        <SignupPage />
      </Layout>
    )
  }

  return (
    <Layout>
      <p>Coming soon</p>
    </Layout>
  )
}

export default App
