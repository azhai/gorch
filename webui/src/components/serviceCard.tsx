import { useState } from 'react'
import { ServiceStatus, startService, stopService, restartService } from '../api/client'

interface ServiceCardProps {
  service: ServiceStatus
  onAction: () => void
  showToast: (msg: string, type?: 'success' | 'error') => void
}

const statusColors: Record<string, string> = {
  running: 'bg-emerald-400',
  stopped: 'bg-gray-300',
  failed: 'bg-red-400',
  crashed: 'bg-red-600',
  starting: 'bg-amber-400',
}

export function ServiceCard({ service, onAction, showToast }: ServiceCardProps) {
  const [actionLoading, setActionLoading] = useState(false)

  const handleAction = async (action: 'start' | 'stop' | 'restart') => {
    setActionLoading(true)
    try {
      switch (action) {
        case 'start':
          await startService(service.name)
          break
        case 'stop':
          await stopService(service.name)
          break
        case 'restart':
          await restartService(service.name)
          break
      }
      showToast(`${service.name} ${action}ed`)
      onAction()
    } catch (err) {
      showToast(`Failed to ${action} ${service.name}: ${err}`, 'error')
    } finally {
      setActionLoading(false)
    }
  }

  const formatUptime = (seconds: number) => {
    if (!seconds) return '-'
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    const s = seconds % 60
    return h > 0 ? `${h}h ${m}m` : `${m}m ${s}s`
  }

  return (
    <div className="bg-white rounded-xl shadow-sm border border-macaron-peach/60 p-4 hover:shadow-md transition-shadow">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <span className={`w-2.5 h-2.5 rounded-full ${statusColors[service.status] || 'bg-gray-300'}`} />
          <h3 className="font-medium text-gray-800">{service.name}</h3>
        </div>
        <span className="text-xs text-gray-500 uppercase tracking-wide">{service.status}</span>
      </div>

      <div className="grid grid-cols-3 gap-2 text-xs text-gray-500 mb-3">
        <div>
          <span className="block text-gray-400">PID</span>
          <span className="text-gray-700">{service.pid || '-'}</span>
        </div>
        <div>
          <span className="block text-gray-400">Uptime</span>
          <span className="text-gray-700">{formatUptime(service.uptime)}</span>
        </div>
        <div>
          <span className="block text-gray-400">Memory (RSS)</span>
          <span className="text-gray-700">
            {service.memoryMB ? `${service.memoryMB} MB` : '-'}
          </span>
        </div>
      </div>

      <div className="flex gap-2">
        {service.status !== 'running' && (
          <button
            onClick={() => handleAction('start')}
            disabled={actionLoading}
            className="px-3 py-1 text-xs bg-emerald-100 text-emerald-700 rounded-lg hover:bg-emerald-200 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          >
            Start
          </button>
        )}
        {service.status === 'running' && (
          <button
            onClick={() => handleAction('stop')}
            disabled={actionLoading}
            className="px-3 py-1 text-xs bg-red-100 text-red-700 rounded-lg hover:bg-red-200 transition-colors disabled:opacity-40 disabled:cursor-not-allowed inline-flex items-center gap-1"
          >
            {actionLoading && <span className="inline-block w-3 h-3 border border-red-400/30 border-t-red-600 rounded-full animate-spin" />}
            Stop
          </button>
        )}
        <button
          onClick={() => handleAction('restart')}
          disabled={actionLoading}
          className="px-3 py-1 text-xs bg-macaron-sky text-blue-700 rounded-lg hover:bg-blue-200 transition-colors disabled:opacity-40 disabled:cursor-not-allowed inline-flex items-center gap-1"
        >
          {actionLoading && <span className="inline-block w-3 h-3 border border-blue-300 border-t-blue-600 rounded-full animate-spin" />}
          Restart
        </button>
      </div>
    </div>
  )
}