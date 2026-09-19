import { BrowserRouter, Route, Routes } from 'react-router'
import AuthProvider from './auth.tsx'
import HomePage from './HomePage.tsx'
import Layout from './Layout.tsx'
import LoginPage from './LoginPage.tsx'
import RequireAuth from './RequireAuth.tsx'
import SignupPage from './SignupPage.tsx'

// App routes the paths the server hands this shell, which are the ones no
// explicit Go route claims: a page Go still renders itself never reaches here,
// so it has no Route below and "*" is what an unclaimed path renders as. The
// routes listed here are therefore also the answer to whether a link may be a
// Link — a path with no Route is reached by leaving this app.
function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Layout>
          <Routes>
            <Route
              path="/"
              element={
                <RequireAuth>
                  <HomePage />
                </RequireAuth>
              }
            />
            <Route path="/login" element={<LoginPage />} />
            <Route path="/signup" element={<SignupPage />} />
            <Route path="*" element={<p>Coming soon</p>} />
          </Routes>
        </Layout>
      </AuthProvider>
    </BrowserRouter>
  )
}

export default App
