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
  const handleAction = async (action: 'start' | 'stop' | 'restart') => {
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
    }
  }

  const formatUptime = (seconds: number) => {
    if (!seconds) return '-'
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    return h > 0 ? `${h}h ${m}m` : `${m}m`
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
          <span className="block text-gray-400">Restarts</span>
          <span className="text-gray-700">{service.restartCount}</span>
        </div>
      </div>

      <div className="flex gap-2">
        {service.status !== 'running' && (
          <button
            onClick={() => handleAction('start')}
            className="px-3 py-1 text-xs bg-emerald-100 text-emerald-700 rounded-lg hover:bg-emerald-200 transition-colors"
          >
            Start
          </button>
        )}
        {service.status === 'running' && (
          <button
            onClick={() => handleAction('stop')}
            className="px-3 py-1 text-xs bg-red-100 text-red-700 rounded-lg hover:bg-red-200 transition-colors"
          >
            Stop
          </button>
        )}
        <button
          onClick={() => handleAction('restart')}
          className="px-3 py-1 text-xs bg-macaron-sky text-blue-700 rounded-lg hover:bg-blue-200 transition-colors"
        >
          Restart
        </button>
      </div>
    </div>
  )
}