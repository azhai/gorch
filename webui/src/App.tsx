import { BrowserRouter, Routes, Route, Link, Outlet, useNavigate } from 'react-router-dom'
import Dashboard from './views/dashboard'
import Logs from './views/logs'
import Config from './views/config'
import TOTPSettings from './views/totp'
import LoginView from './views/login'
import AuthGuard from './components/authGuard'
import { useSSE } from './hooks/useSSE'
import { isAuthEnabled, getToken, removeToken } from './api/authStore'
import { ROUTER_BASENAME, STATIC_PREFIX } from './api/config'

function AppLayout() {
  const { connected } = useSSE()
  const navigate = useNavigate()

  const handleLogout = () => {
    removeToken()
    navigate('/login')
  }

  const showLogout = isAuthEnabled() && getToken()

  return (
    <div className="min-h-screen bg-macaron-cream">
      <header className="bg-white/80 backdrop-blur-sm border-b border-macaron-peach sticky top-0 z-10">
        <div className="max-w-7xl mx-auto px-4 py-3 flex items-center justify-between">
          <div className="flex items-center gap-6">
            <div className="flex items-center gap-2">
              <img src={`${STATIC_PREFIX}/logo.svg`} alt="Gorch" className="h-6 w-6" />
              <h1 className="text-xl font-semibold text-gray-800">Gorch</h1>
            </div>
            <nav className="flex gap-4">
              <Link to="/" className="text-sm text-gray-600 hover:text-gray-900 transition-colors">
                Dashboard
              </Link>
              <Link to="/logs" className="text-sm text-gray-600 hover:text-gray-900 transition-colors">
                Logs
              </Link>
              <Link to="/config" className="text-sm text-gray-600 hover:text-gray-900 transition-colors">
                Config
              </Link>
              {showLogout && (
                <Link to="/totp" className="text-sm text-gray-600 hover:text-gray-900 transition-colors">
                  Security
                </Link>
              )}
            </nav>
          </div>
          <div className="flex items-center gap-2">
            <span
              className={`w-2.5 h-2.5 rounded-full ${
                connected ? 'bg-emerald-400' : 'bg-red-400'
              }`}
            />
            <span className="text-xs text-gray-500">
              {connected ? 'Connected' : 'Reconnecting...'}
            </span>
            {showLogout && (
              <button
                onClick={handleLogout}
                className="text-xs text-gray-500 hover:text-gray-700 ml-2"
              >
                Logout
              </button>
            )}
          </div>
        </div>
      </header>

      <main className="max-w-5xl mx-auto px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}

export default function App() {
  return (
    <BrowserRouter basename={ROUTER_BASENAME}>
      <Routes>
        <Route path="/login" element={<LoginView />} />
        <Route
          element={
            <AuthGuard>
              <AppLayout />
            </AuthGuard>
          }
        >
          <Route path="/" element={<Dashboard />} />
          <Route path="/logs" element={<Logs />} />
          <Route path="/config" element={<Config />} />
          <Route path="/totp" element={<TOTPSettings />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
