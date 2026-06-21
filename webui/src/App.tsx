import { BrowserRouter, Routes, Route, Link, Outlet, useNavigate } from 'react-router-dom'
import Dashboard from './views/dashboard'
import Logs from './views/logs'
import Config from './views/config'
import LoginView from './views/login'
import AuthGuard from './components/authGuard'
import { useWs } from './hooks/useWs'
import { isAuthEnabled, getToken, removeToken } from './api/authStore'

function AppLayout() {
  const { connected } = useWs()
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
            <h1 className="text-xl font-semibold text-gray-800">Gorch</h1>
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

      <main className="max-w-7xl mx-auto px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}

export default function App() {
  return (
    <BrowserRouter>
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
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
