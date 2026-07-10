import { useState, useEffect, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { setToken, isAuthEnabled, getToken } from '../api/authStore'
import { API_BASE } from '../api/config'
import { useI18n, Lang } from '../i18n/I18nProvider'
import { useTheme, ColorTheme } from '../i18n/ThemeProvider'

const LANG_OPTIONS: { value: Lang; label: string }[] = [
  { value: 'en', label: 'English' },
  { value: 'zh-CN', label: '简体中文' },
]

type ThemeOption = 'orange' | 'mint' | 'pinkblue' | 'monet' | 'dark'

const THEME_OPTIONS: { value: ThemeOption; labelKey: string }[] = [
  { value: 'orange', labelKey: 'theme.orange' },
  { value: 'mint', labelKey: 'theme.mint' },
  { value: 'pinkblue', labelKey: 'theme.pinkblue' },
  { value: 'monet', labelKey: 'theme.monet' },
  { value: 'dark', labelKey: 'theme.dark' },
]

export default function LoginView() {
  const navigate = useNavigate()
  const { t, lang, setLang } = useI18n()
  const { colorTheme, mode, setColorTheme, setMode } = useTheme()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [step, setStep] = useState<'credentials' | 'totp'>('credentials')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

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

  useEffect(() => {
    if (isAuthEnabled() && getToken()) {
      navigate('/')
    }
  }, [navigate])

  const handleCredentialsSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!username.trim() || !password.trim()) {
      setError(t('login.enterCredentials'))
      return
    }

    setLoading(true)
    setError(null)

    try {
      const res = await fetch(`${API_BASE}/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      })

      const json = await res.json()

      if (res.ok && json.success) {
        if (json.data?.requireTotp) {
          setStep('totp')
        } else if (json.data?.token) {
          setToken(json.data.token)
          navigate('/')
        }
      } else if (res.status === 401) {
        setError(t('login.invalidCredentials'))
      } else {
        setError(json.message || t('login.networkError'))
      }
    } catch {
      setError(t('login.networkError'))
    } finally {
      setLoading(false)
    }
  }

  const handleTotpSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!totpCode.trim() || totpCode.length !== 6) {
      setError(t('login.enter6DigitCode'))
      return
    }

    setLoading(true)
    setError(null)

    try {
      const res = await fetch(`${API_BASE}/auth/login/totp`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, code: totpCode }),
      })

      const json = await res.json()

      if (res.ok && json.success && json.data?.token) {
        setToken(json.data.token)
        navigate('/')
      } else {
        setError(json.message || t('login.invalidCode'))
      }
    } catch {
      setError(t('login.networkError'))
    } finally {
      setLoading(false)
    }
  }

  const handleBack = () => {
    setStep('credentials')
    setTotpCode('')
    setError(null)
  }

  return (
    <div className="min-h-screen bg-macaron-cream dark:bg-gray-900 flex items-center justify-center py-12 px-4 transition-colors">
      <div className="bg-white dark:bg-gray-800 rounded-xl shadow-lg border border-macaron-peach dark:border-gray-700 w-full max-w-md p-8 transition-colors">
        <h1 className="text-2xl font-semibold text-gray-800 dark:text-gray-100 text-center mb-6">{t('login.title')}</h1>

        <div className="flex flex-wrap items-center justify-end gap-3 mb-4">
          <div className="flex items-center gap-2">
            <label htmlFor="login-lang" className="text-xs text-gray-500 dark:text-gray-400 whitespace-nowrap">
              {t('login.language')}
            </label>
            <select
              id="login-lang"
              value={lang}
              onChange={(e) => setLang(e.target.value as Lang)}
              className="text-xs border border-gray-200 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200 rounded-lg px-2 py-1 bg-white focus:outline-none focus:ring-1 focus:ring-macaron-peach"
            >
              {LANG_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
          </div>
          <div className="flex items-center gap-2">
            <label htmlFor="login-theme" className="text-xs text-gray-500 dark:text-gray-400 whitespace-nowrap">
              {t('login.theme')}
            </label>
            <select
              id="login-theme"
              value={currentTheme}
              onChange={(e) => handleThemeChange(e.target.value as ThemeOption)}
              className="text-xs border border-gray-200 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200 rounded-lg px-2 py-1 bg-white focus:outline-none focus:ring-1 focus:ring-macaron-peach"
            >
              {THEME_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {t(opt.labelKey)}
                </option>
              ))}
            </select>
          </div>
        </div>

        {error && (
          <div className="bg-macaron-rose/30 dark:bg-red-900/30 text-red-700 dark:text-red-300 text-sm rounded-lg px-4 py-3 mb-4">
            {error}
          </div>
        )}

        {step === 'credentials' ? (
          <form onSubmit={handleCredentialsSubmit} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">{t('login.username')}</label>
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                className="w-full px-3 py-2 border border-macaron-peach dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50 text-sm"
                placeholder={t('login.usernamePlaceholder')}
                disabled={loading}
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">{t('login.password')}</label>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="w-full px-3 py-2 border border-macaron-peach dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50 text-sm"
                placeholder={t('login.passwordPlaceholder')}
                disabled={loading}
              />
            </div>

            <button
              type="submit"
              disabled={loading}
              className="w-full py-2 px-4 bg-macaron-orange/80 hover:bg-macaron-orange text-white font-medium rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed text-sm"
            >
              {loading ? t('login.signingIn') : t('login.signIn')}
            </button>
          </form>
        ) : (
          <form onSubmit={handleTotpSubmit} className="space-y-4">
            <div className="text-center mb-4">
              <p className="text-sm text-gray-600 dark:text-gray-300">{t('login.totpTitle')}</p>
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">{t('login.verificationCode')}</label>
              <input
                type="text"
                value={totpCode}
                onChange={(e) => setTotpCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                className="w-full px-3 py-2 border border-macaron-peach dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50 text-sm text-center tracking-widest"
                placeholder={t('login.totpPlaceholder')}
                disabled={loading}
                maxLength={6}
              />
            </div>

            <button
              type="submit"
              disabled={loading || totpCode.length !== 6}
              className="w-full py-2 px-4 bg-macaron-orange/80 hover:bg-macaron-orange text-white font-medium rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed text-sm"
            >
              {loading ? t('login.verifying') : t('login.verify')}
            </button>

            <button
              type="button"
              onClick={handleBack}
              className="w-full py-2 px-4 text-gray-600 dark:text-gray-400 text-sm hover:text-gray-800 dark:hover:text-gray-200 transition-colors"
            >
              {t('login.back')}
            </button>
          </form>
        )}
      </div>
    </div>
  )
}
