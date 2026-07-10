import { BrowserRouter, Routes, Route, Link, Outlet, useNavigate } from 'react-router-dom'
import { useMemo } from 'react'
import Dashboard from './views/dashboard'
import Logs from './views/logs'
import Config from './views/config'
import TOTPSettings from './views/totp'
import LoginView from './views/login'
import AuthGuard from './components/authGuard'
import { useSSE } from './hooks/useSSE'
import { isAuthEnabled, getToken, removeToken } from './api/authStore'
import { ROUTER_BASENAME, STATIC_PREFIX } from './api/config'
import { I18nProvider, useI18n } from './i18n/I18nProvider'
import { ThemeProvider, useTheme, ColorTheme } from './i18n/ThemeProvider'

type ThemeOption = 'orange' | 'mint' | 'pinkblue' | 'monet' | 'dark'

const THEME_OPTIONS: { value: ThemeOption; labelKey: string }[] = [
  { value: 'orange', labelKey: 'theme.orange' },
  { value: 'mint', labelKey: 'theme.mint' },
  { value: 'pinkblue', labelKey: 'theme.pinkblue' },
  { value: 'monet', labelKey: 'theme.monet' },
  { value: 'dark', labelKey: 'theme.dark' },
]

function AppLayout() {
  const { connected } = useSSE()
  const navigate = useNavigate()
  const { t } = useI18n()
  const { colorTheme, mode, setColorTheme, setMode } = useTheme()

  const currentTheme = useMemo<ThemeOption>(() => {
    if (mode === 'dark') return 'dark'
    return colorTheme
  }, [colorTheme, mode])

  const handleThemeChange = (value: ThemeOption) => {
    if (value === 'dark') {
      setMode('dark')
    } else {
      setMode('light')
      setColorTheme(value as ColorTheme)
    }
  }

  const handleLogout = () => {
    removeToken()
    navigate('/login')
  }

  const showLogout = isAuthEnabled() && getToken()

  return (
    <div className="min-h-screen bg-macaron-cream dark:bg-gray-900 transition-colors">
      <header className="bg-white/80 dark:bg-gray-800/80 backdrop-blur-sm border-b border-macaron-peach dark:border-gray-700 sticky top-0 z-10">
        <div className="max-w-7xl mx-auto px-4 py-3 flex items-center justify-between">
          <div className="flex items-center gap-6">
            <div className="flex items-center gap-2">
              <img src={`${STATIC_PREFIX}/logo.svg`} alt="Gorch" className="h-6 w-6" />
              <h1 className="text-xl font-semibold text-gray-800 dark:text-gray-100">Gorch</h1>
            </div>
            <nav className="flex gap-4">
              <Link to="/" className="text-sm text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white transition-colors">
                {t('nav.dashboard')}
              </Link>
              <Link to="/logs" className="text-sm text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white transition-colors">
                {t('nav.logs')}
              </Link>
              <Link to="/config" className="text-sm text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white transition-colors">
                {t('nav.config')}
              </Link>
              {showLogout && (
                <Link to="/totp" className="text-sm text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white transition-colors">
                  {t('nav.security')}
                </Link>
              )}
            </nav>
          </div>
          <div className="flex items-center gap-3">
            <select
              value={currentTheme}
              onChange={(e) => handleThemeChange(e.target.value as ThemeOption)}
              className="text-xs border border-gray-200 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200 rounded-lg px-2 py-1 bg-white focus:outline-none focus:ring-1 focus:ring-macaron-peach"
              aria-label={t('login.theme')}
            >
              {THEME_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {t(opt.labelKey)}
                </option>
              ))}
            </select>
            <span
              className={`w-2.5 h-2.5 rounded-full ${
                connected ? 'bg-emerald-400' : 'bg-red-400'
              }`}
            />
            <span className="text-xs text-gray-500 dark:text-gray-400">
              {connected ? t('status.connected') : t('status.reconnecting')}
            </span>
            {showLogout && (
              <button
                onClick={handleLogout}
                className="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 ml-2"
              >
                {t('nav.logout')}
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

function RootApp() {
  return (
    <ThemeProvider>
      <I18nProvider>
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
      </I18nProvider>
    </ThemeProvider>
  )
}

export default RootApp
