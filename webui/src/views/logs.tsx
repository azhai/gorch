import { useState, useEffect } from 'react'
import { fetchServices, fetchLogs, ServiceStatus, LogResponse } from '../api/client'

export default function Logs() {
  const [services, setServices] = useState<ServiceStatus[]>([])
  const [selected, setSelected] = useState<string>('')
  const [logData, setLogData] = useState<LogResponse | null>(null)

  useEffect(() => {
    fetchServices().then(setServices)
  }, [])

  useEffect(() => {
    if (!selected) {
      setLogData(null)
      return
    }
    fetchLogs(selected, 500).then(setLogData)
  }, [selected])

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
      </div>

      {logData && logData.logPath && (
        <div className="mb-2 text-xs text-gray-400">
          Source: {logData.logPath}
        </div>
      )}

      <div className="bg-gray-900 rounded-xl p-4 max-h-[600px] overflow-y-auto">
        {!logData || logData.lines.length === 0 ? (
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
