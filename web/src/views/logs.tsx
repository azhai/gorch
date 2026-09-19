import { useState, useEffect } from 'react'
import { fetchServices, fetchLogs, clearLogs, ServiceStatus, LogResponse } from '../api/client'
import { useI18n } from '../i18n/I18nProvider'

type LogType = 'stdout' | 'stderr'

export default function Logs() {
  const [services, setServices] = useState<ServiceStatus[]>([])
  const [selected, setSelected] = useState<string>('')
  const [logType, setLogType] = useState<LogType>('stdout')
  const [logData, setLogData] = useState<LogResponse | null>(null)
  const [clearing, setClearing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { t } = useI18n()

  useEffect(() => {
    fetchServices().then(setServices)
  }, [])

  useEffect(() => {
    if (!selected) {
      setLogData(null)
      return
    }
    fetchLogs(selected, 500, logType).then(setLogData)
  }, [selected, logType])

  const handleClear = async () => {
    if (!selected) return
    setClearing(true)
    setError(null)
    try {
      await clearLogs(selected, logType)
      setLogData((prev) => prev ? { ...prev, lines: [] } : null)
    } catch (err) {
      setError(t('logs.failedClear'))
    } finally {
      setClearing(false)
    }
  }

  return (
    <div>
      <div className="flex items-center gap-4 mb-4">
        <h2 className="text-lg font-semibold text-gray-800 dark:text-gray-100">{t('logs.title')}</h2>
        <select
          value={selected}
          aria-label="Select service to view logs"
          onChange={(e) => setSelected(e.target.value)}
          className="px-3 py-1.5 text-sm border border-macaron-peach dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200 rounded-lg bg-white focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
        >
          <option value="">{t('logs.selectService')}</option>
          {services.map((s) => (
            <option key={s.name} value={s.name}>{s.name}</option>
          ))}
        </select>

        {selected && (
          <div className="flex rounded-lg border border-macaron-peach dark:border-gray-600 overflow-hidden">
            {(['stdout', 'stderr'] as LogType[]).map((type) => (
              <button
                key={type}
                onClick={() => setLogType(type)}
                className={`px-3 py-1.5 text-xs font-medium transition-colors ${
                  logType === type
                    ? 'bg-macaron-orange text-white'
                    : 'bg-white dark:bg-gray-700 text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white'
                }`}
              >
                {type === 'stdout' ? t('logs.stdout') : t('logs.stderr')}
              </button>
            ))}
          </div>
        )}

        {selected && (
          <button
            onClick={handleClear}
            disabled={clearing}
            className="px-3 py-1.5 text-xs bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 rounded-lg hover:bg-red-200 dark:hover:bg-red-900/60 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {clearing ? t('logs.clearing') : t('logs.clear')}
          </button>
        )}
      </div>

      {error && (
        <div className="mb-2 bg-macaron-rose/30 dark:bg-red-900/30 text-red-700 dark:text-red-300 text-sm rounded-lg px-4 py-2">
          {error}
        </div>
      )}

      {logData && logData.logPath && (
        <div className="mb-2 text-xs text-gray-400 dark:text-gray-500">
          {t('logs.source')}: {logData.logPath}
        </div>
      )}

      <div className="bg-gray-900 dark:bg-gray-950 rounded-xl p-4 max-h-[600px] overflow-y-auto">
        {!logData || !logData.lines || logData.lines.length === 0 ? (
          <p className="text-gray-500 text-sm">{t('logs.noLogs')}</p>
        ) : (
          logData.lines.map((line, i) => (
            <div key={i} className="text-xs font-mono text-gray-300 leading-5 whitespace-pre-wrap">
              {line}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
