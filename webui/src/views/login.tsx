import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { setToken, isAuthEnabled, getToken } from '../api/authStore'
import { API_BASE } from '../api/config'

export default function LoginView() {
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [step, setStep] = useState<'credentials' | 'totp'>('credentials')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (isAuthEnabled() && getToken()) {
      navigate('/')
    }
  }, [navigate])

  const handleCredentialsSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!username.trim() || !password.trim()) {
      setError('Please enter username and password')
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
        setError('Invalid username or password')
      } else {
        setError(json.message || 'Login failed')
      }
    } catch {
      setError('Network error, please try again')
    } finally {
      setLoading(false)
    }
  }

  const handleTotpSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!totpCode.trim() || totpCode.length !== 6) {
      setError('Please enter 6-digit code')
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
        setError(json.message || 'Invalid verification code')
      }
    } catch {
      setError('Network error, please try again')
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
    <div className="min-h-screen bg-macaron-cream flex items-center justify-center py-12 px-4">
      <div className="bg-white rounded-xl shadow-lg border border-macaron-peach/60 w-full max-w-md p-8">
        <h1 className="text-2xl font-semibold text-gray-800 text-center mb-6">Gorch</h1>

        {error && (
          <div className="bg-macaron-rose/30 text-red-700 text-sm rounded-lg px-4 py-3 mb-4">
            {error}
          </div>
        )}

        {step === 'credentials' ? (
          <form onSubmit={handleCredentialsSubmit} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Username</label>
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                className="w-full px-3 py-2 border border-macaron-peach rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50 text-sm"
                placeholder="Enter username"
                disabled={loading}
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Password</label>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="w-full px-3 py-2 border border-macaron-peach rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50 text-sm"
                placeholder="Enter password"
                disabled={loading}
              />
            </div>

            <button
              type="submit"
              disabled={loading}
              className="w-full py-2 px-4 bg-macaron-orange/80 hover:bg-macaron-orange text-white font-medium rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed text-sm"
            >
              {loading ? 'Signing in...' : 'Sign in'}
            </button>
          </form>
        ) : (
          <form onSubmit={handleTotpSubmit} className="space-y-4">
            <div className="text-center mb-4">
              <p className="text-sm text-gray-600">Enter the 6-digit code from your authenticator app</p>
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Verification Code</label>
              <input
                type="text"
                value={totpCode}
                onChange={(e) => setTotpCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                className="w-full px-3 py-2 border border-macaron-peach rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50 text-sm text-center tracking-widest"
                placeholder="000000"
                disabled={loading}
                maxLength={6}
              />
            </div>

            <button
              type="submit"
              disabled={loading || totpCode.length !== 6}
              className="w-full py-2 px-4 bg-macaron-orange/80 hover:bg-macaron-orange text-white font-medium rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed text-sm"
            >
              {loading ? 'Verifying...' : 'Verify'}
            </button>

            <button
              type="button"
              onClick={handleBack}
              className="w-full py-2 px-4 text-gray-600 text-sm hover:text-gray-800 transition-colors"
            >
              Back
            </button>
          </form>
        )}
      </div>
    </div>
  )
}
