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
import { useI18n } from '../i18n/I18nProvider'

export default function TOTPSettings() {
  const [status, setStatus] = useState<TOTPStatus | null>(null)
  const [setupData, setSetupData] = useState<TOTPSetupResponse | null>(null)
  const [verifyCode, setVerifyCode] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const { t } = useI18n()

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
      setError(t('totp.failedLoad'))
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
        setError(res.message || t('totp.setupFailed'))
      }
    } catch (e) {
      setError(t('totp.failedSetup'))
    } finally {
      setLoading(false)
    }
  }

  async function handleVerifySetup() {
    if (!verifyCode) {
      setError(t('totp.enterCode'))
      return
    }
    setLoading(true)
    setError('')
    try {
      const res = await totpVerifySetup(verifyCode)
      if (res.success) {
        setSuccess(t('totp.enabledSuccess'))
        setSetupData(null)
        setVerifyCode('')
        loadStatus()
      } else {
        setError(res.message || t('totp.verifyFailed'))
      }
    } catch (e) {
      setError(t('totp.failedVerify'))
    } finally {
      setLoading(false)
    }
  }

  async function handleDisable() {
    if (!confirm(t('totp.disableConfirm'))) {
      return
    }
    setLoading(true)
    setError('')
    try {
      const res = await totpDisable()
      if (res.success) {
        setSuccess(t('totp.disabledSuccess'))
        loadStatus()
      } else {
        setError(res.message || t('totp.failedDisable'))
      }
    } catch (e) {
      setError(t('totp.failedDisable'))
    } finally {
      setLoading(false)
    }
  }

  async function handleRegenerateBackupCodes() {
    if (!confirm(t('totp.regenerateConfirm'))) {
      return
    }
    setLoading(true)
    setError('')
    try {
      const res = await totpRegenerateBackupCodes()
      if (res.success && res.data) {
        setSetupData({ ...setupData!, backupCodes: res.data.backupCodes })
        setSuccess(t('totp.regenerated'))
        loadStatus()
      } else {
        setError(res.message || t('totp.failedRegenerate'))
      }
    } catch (e) {
      setError(t('totp.failedRegenerate'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="max-w-2xl mx-auto">
      <h2 className="text-2xl font-semibold text-gray-800 dark:text-gray-100 mb-6">{t('totp.title')}</h2>

      {error && (
        <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded text-red-700 dark:text-red-300 text-sm">
          {error}
        </div>
      )}

      {success && (
        <div className="mb-4 p-3 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded text-green-700 dark:text-green-300 text-sm">
          {success}
        </div>
      )}

      {loading && !setupData && (
        <div className="text-gray-500 dark:text-gray-400 text-sm">{t('totp.loading')}</div>
      )}

      {!loading && !setupData && status && (
        <div className="space-y-4">
          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4">
            <div className="flex items-center justify-between">
              <div>
                <div className="font-medium text-gray-800 dark:text-gray-100">{t('totp.status')}</div>
                <div className="text-sm text-gray-500 dark:text-gray-400 mt-1">
                  {status.enabled ? t('totp.enabled') : t('totp.notEnabled')}
                </div>
              </div>
              <span
                className={`px-2 py-1 rounded text-xs font-medium ${
                  status.enabled
                    ? 'bg-green-100 dark:bg-green-900/40 text-green-700 dark:text-green-300'
                    : 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300'
                }`}
              >
                {status.enabled ? t('totp.enabledBadge') : t('totp.disabledBadge')}
              </span>
            </div>
          </div>

          {status.enabled && (
            <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4">
              <div className="font-medium text-gray-800 dark:text-gray-100 mb-2">{t('totp.backupCodes')}</div>
              <div className="text-sm text-gray-500 dark:text-gray-400 mb-3">
                {t('totp.backupRemaining', { count: status.backupCodes })}
              </div>
              <button
                onClick={handleRegenerateBackupCodes}
                disabled={loading}
                className="px-3 py-1.5 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded transition disabled:opacity-50 text-gray-700 dark:text-gray-200"
              >
                {t('totp.regenerate')}
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
                {t('totp.enable')}
              </button>
            )}
            {status.enabled && (
              <button
                onClick={handleDisable}
                disabled={loading}
                className="px-4 py-2 bg-red-600 hover:bg-red-700 text-white rounded transition disabled:opacity-50"
              >
                {t('totp.disable')}
              </button>
            )}
          </div>
        </div>
      )}

      {setupData && (
        <div className="space-y-6">
          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-6">
            <h3 className="font-medium text-gray-800 dark:text-gray-100 mb-4">{t('totp.scanQr')}</h3>
            <div className="flex flex-col items-center gap-4">
              <img
                src={setupData.qrCode}
                alt="TOTP QR Code"
                className="w-48 h-48 border border-gray-200 dark:border-gray-600 rounded"
              />
              <div className="text-xs text-gray-500 dark:text-gray-400 text-center">
                {t('totp.scanQrHelp')}
              </div>
            </div>
          </div>

          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-6">
            <h3 className="font-medium text-gray-800 dark:text-gray-100 mb-4">{t('totp.manualEntry')}</h3>
            <div className="bg-gray-50 dark:bg-gray-700 p-3 rounded font-mono text-sm break-all text-gray-800 dark:text-gray-200">
              {setupData.secret}
            </div>
          </div>

          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-6">
            <h3 className="font-medium text-gray-800 dark:text-gray-100 mb-4">{t('totp.backupCodes')}</h3>
            <div className="text-sm text-gray-500 dark:text-gray-400 mb-3">
              {t('totp.backupSaveHint')}
            </div>
            <div className="grid grid-cols-2 gap-2">
              {setupData.backupCodes.map((code, i) => (
                <div
                  key={i}
                  className="bg-gray-50 dark:bg-gray-700 p-2 rounded font-mono text-sm text-center text-gray-800 dark:text-gray-200"
                >
                  {code}
                </div>
              ))}
            </div>
          </div>

          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-6">
            <h3 className="font-medium text-gray-800 dark:text-gray-100 mb-4">{t('totp.verifySetup')}</h3>
            <div className="text-sm text-gray-500 dark:text-gray-400 mb-3">
              {t('totp.verifySetupHelp')}
            </div>
            <div className="flex gap-3">
              <input
                type="text"
                value={verifyCode}
                onChange={(e) => setVerifyCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                placeholder="000000"
                className="flex-1 px-3 py-2 border border-gray-200 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200 rounded text-center font-mono text-lg tracking-widest"
                maxLength={6}
              />
              <button
                onClick={handleVerifySetup}
                disabled={loading || verifyCode.length !== 6}
                className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded transition disabled:opacity-50"
              >
                {t('totp.verifyButton')}
              </button>
            </div>
          </div>

          <button
            onClick={() => {
              setSetupData(null)
              setVerifyCode('')
            }}
            className="text-sm text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
          >
            {t('totp.cancelSetup')}
          </button>
        </div>
      )}
    </div>
  )
}