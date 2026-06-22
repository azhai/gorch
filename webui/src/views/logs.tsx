import { useState, useEffect } from 'react'
import { fetchServices, fetchLogs, clearLogs, ServiceStatus, LogResponse } from '../api/client'

type LogType = 'stdout' | 'stderr'

export default function Logs() {
  const [services, setServices] = useState<ServiceStatus[]>([])
  const [selected, setSelected] = useState<string>('')
  const [logType, setLogType] = useState<LogType>('stdout')
  const [logData, setLogData] = useState<LogResponse | null>(null)
  const [clearing, setClearing] = useState(false)

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
    try {
      await clearLogs(selected, logType)
      setLogData((prev) => prev ? { ...prev, lines: [] } : null)
    } catch {
      // ignore
    } finally {
      setClearing(false)
    }
  }

  return (
    <div>
      <div className="flex items-center gap-4 mb-4">
        <h2 className="text-lg font-semibold text-gray-800">Logs</h2>
        <select
          value={selected}
          onChange={(e) => setSelected(e.target.value)}
          className="px-3 py-1.5 text-sm border border-macaron-peach rounded-lg bg-white focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
        >
          <option value="">Select service...</option>
          {services.map((s) => (
            <option key={s.name} value={s.name}>{s.name}</option>
          ))}
        </select>

        {selected && (
          <div className="flex rounded-lg border border-macaron-peach overflow-hidden">
            {(['stdout', 'stderr'] as LogType[]).map((type) => (
              <button
                key={type}
                onClick={() => setLogType(type)}
                className={`px-3 py-1.5 text-xs font-medium transition-colors ${
                  logType === type
                    ? 'bg-macaron-orange text-white'
                    : 'bg-white text-gray-600 hover:text-gray-900'
                }`}
              >
                {type.toUpperCase()}
              </button>
            ))}
          </div>
        )}

        {selected && (
          <button
            onClick={handleClear}
            disabled={clearing}
            className="px-3 py-1.5 text-xs bg-red-100 text-red-700 rounded-lg hover:bg-red-200 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {clearing ? 'Clearing...' : 'Clear'}
          </button>
        )}
      </div>

      {logData && logData.logPath && (
        <div className="mb-2 text-xs text-gray-400">
          Source: {logData.logPath}
        </div>
      )}

      <div className="bg-gray-900 rounded-xl p-4 max-h-[600px] overflow-y-auto">
        {!logData || !logData.lines || logData.lines.length === 0 ? (
          <p className="text-gray-500 text-sm">No logs available</p>
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
