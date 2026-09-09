import Layout from './Layout.tsx'
import LoginPage from './LoginPage.tsx'

// App picks a page by the browser's current path. There is no router yet:
// the server only serves this shell for a path no explicit route claims,
// and /login is the one page converted so far.
function App() {
  if (window.location.pathname === '/login') {
    return (
      <Layout>
        <LoginPage />
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
