import { useState, useEffect } from 'react'
import {
  totpSetup,
  totpVerifySetup,
  totpStatus,
  totpDisable,
  totpRegenerateBackupCodes,
  TOTPSetupResponse,
  TOTPStatus,

} from '../api/client'

export default function TOTPSettings() {
  const [status, setStatus] = useState<TOTPStatus | null>(null)
  const [setupData, setSetupData] = useState<TOTPSetupResponse | null>(null)
  const [verifyCode, setVerifyCode] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')

  useEffect(() => {
    loadStatus()
  }, [])

  async function loadStatus() {
    setLoading(true)
    try {
      const res = await totpStatus()
      if (res.success && res.data) {
        setStatus(res.data)
      } else {
        setStatus({ enabled: false, backupCodes: 0, hasBinding: false })
      }
    } catch (e) {
      setError('Failed to load TOTP status')
    } finally {
      setLoading(false)
    }
  }

  async function handleSetup() {
    setLoading(true)
    setError('')
    setSuccess('')
    try {
      const res = await totpSetup()
      if (res.success && res.data) {
        setSetupData(res.data)
      } else {
        setError(res.message || 'Setup failed')
      }
    } catch (e) {
      setError('Failed to start TOTP setup')
    } finally {
      setLoading(false)
    }
  }

  async function handleVerifySetup() {
    if (!verifyCode) {
      setError('Please enter verification code')
      return
    }
    setLoading(true)
    setError('')
    try {
      const res = await totpVerifySetup(verifyCode)
      if (res.success) {
        setSuccess('TOTP enabled successfully')
        setSetupData(null)
        setVerifyCode('')
        loadStatus()
      } else {
        setError(res.message || 'Verification failed')
      }
    } catch (e) {
      setError('Failed to verify code')
    } finally {
      setLoading(false)
    }
  }

  async function handleDisable() {
    if (!confirm('Are you sure you want to disable TOTP? This will reduce account security.')) {
      return
    }
    setLoading(true)
    setError('')
    try {
      const res = await totpDisable()
      if (res.success) {
        setSuccess('TOTP disabled')
        loadStatus()
      } else {
        setError(res.message || 'Failed to disable TOTP')
      }
    } catch (e) {
      setError('Failed to disable TOTP')
    } finally {
      setLoading(false)
    }
  }

  async function handleRegenerateBackupCodes() {
    if (!confirm('Regenerating backup codes will invalidate existing ones. Continue?')) {
      return
    }
    setLoading(true)
    setError('')
    try {
      const res = await totpRegenerateBackupCodes()
      if (res.success && res.data) {
        setSetupData({ ...setupData!, backupCodes: res.data.backupCodes })
        setSuccess('New backup codes generated')
        loadStatus()
      } else {
        setError(res.message || 'Failed to regenerate backup codes')
      }
    } catch (e) {
      setError('Failed to regenerate backup codes')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="max-w-2xl mx-auto">
      <h2 className="text-2xl font-semibold text-gray-800 mb-6">Two-Factor Authentication</h2>

      {error && (
        <div className="mb-4 p-3 bg-red-50 border border-red-200 rounded text-red-700 text-sm">
          {error}
        </div>
      )}

      {success && (
        <div className="mb-4 p-3 bg-green-50 border border-green-200 rounded text-green-700 text-sm">
          {success}
        </div>
      )}

      {loading && !setupData && (
        <div className="text-gray-500 text-sm">Loading...</div>
      )}

      {!loading && !setupData && status && (
        <div className="space-y-4">
          <div className="bg-white rounded-lg border border-gray-200 p-4">
            <div className="flex items-center justify-between">
              <div>
                <div className="font-medium text-gray-800">Status</div>
                <div className="text-sm text-gray-500 mt-1">
                  {status.enabled ? 'TOTP is enabled' : 'TOTP is not enabled'}
                </div>
              </div>
              <span
                className={`px-2 py-1 rounded text-xs font-medium ${
                  status.enabled
                    ? 'bg-green-100 text-green-700'
                    : 'bg-gray-100 text-gray-600'
                }`}
              >
                {status.enabled ? 'Enabled' : 'Disabled'}
              </span>
            </div>
          </div>

          {status.enabled && (
            <div className="bg-white rounded-lg border border-gray-200 p-4">
              <div className="font-medium text-gray-800 mb-2">Backup Codes</div>
              <div className="text-sm text-gray-500 mb-3">
                {status.backupCodes} unused backup codes remaining
              </div>
              <button
                onClick={handleRegenerateBackupCodes}
                disabled={loading}
                className="px-3 py-1.5 text-sm bg-gray-100 hover:bg-gray-200 rounded transition disabled:opacity-50"
              >
                Regenerate Backup Codes
              </button>
            </div>
          )}

          <div className="flex gap-3">
            {!status.enabled && (
              <button
                onClick={handleSetup}
                disabled={loading}
                className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded transition disabled:opacity-50"
              >
                Enable TOTP
              </button>
            )}
            {status.enabled && (
              <button
                onClick={handleDisable}
                disabled={loading}
                className="px-4 py-2 bg-red-600 hover:bg-red-700 text-white rounded transition disabled:opacity-50"
              >
                Disable TOTP
              </button>
            )}
          </div>
        </div>
      )}

      {setupData && (
        <div className="space-y-6">
          <div className="bg-white rounded-lg border border-gray-200 p-6">
            <h3 className="font-medium text-gray-800 mb-4">Scan QR Code</h3>
            <div className="flex flex-col items-center gap-4">
              <img
                src={setupData.qrCode}
                alt="TOTP QR Code"
                className="w-48 h-48 border border-gray-200 rounded"
              />
              <div className="text-xs text-gray-500 text-center">
                Scan with Google Authenticator, Authy, or similar app
              </div>
            </div>
          </div>

          <div className="bg-white rounded-lg border border-gray-200 p-6">
            <h3 className="font-medium text-gray-800 mb-4">Manual Entry</h3>
            <div className="bg-gray-50 p-3 rounded font-mono text-sm break-all">
              {setupData.secret}
            </div>
          </div>

          <div className="bg-white rounded-lg border border-gray-200 p-6">
            <h3 className="font-medium text-gray-800 mb-4">Backup Codes</h3>
            <div className="text-sm text-gray-500 mb-3">
              Save these codes in a secure location. Each can be used once if you lose access to your authenticator.
            </div>
            <div className="grid grid-cols-2 gap-2">
              {setupData.backupCodes.map((code, i) => (
                <div
                  key={i}
                  className="bg-gray-50 p-2 rounded font-mono text-sm text-center"
                >
                  {code}
                </div>
              ))}
            </div>
          </div>

          <div className="bg-white rounded-lg border border-gray-200 p-6">
            <h3 className="font-medium text-gray-800 mb-4">Verify Setup</h3>
            <div className="text-sm text-gray-500 mb-3">
              Enter the 6-digit code from your authenticator app to complete setup
            </div>
            <div className="flex gap-3">
              <input
                type="text"
                value={verifyCode}
                onChange={(e) => setVerifyCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                placeholder="000000"
                className="flex-1 px-3 py-2 border border-gray-200 rounded text-center font-mono text-lg tracking-widest"
                maxLength={6}
              />
              <button
                onClick={handleVerifySetup}
                disabled={loading || verifyCode.length !== 6}
                className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded transition disabled:opacity-50"
              >
                Verify
              </button>
            </div>
          </div>

          <button
            onClick={() => {
              setSetupData(null)
              setVerifyCode('')
            }}
            className="text-sm text-gray-500 hover:text-gray-700"
          >
            Cancel Setup
          </button>
        </div>
      )}
    </div>
  )
}