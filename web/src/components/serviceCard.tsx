import { useState } from 'react'
import { ServiceStatus, startService, stopService, restartService } from '../api/client'
import { useI18n } from '../i18n/I18nProvider'

interface ServiceCardProps {
  service: ServiceStatus
  onAction: () => void
  showToast: (msg: string, type?: 'success' | 'error') => void
  uptimeMode: 'elapsed' | 'start'
}

const statusColors: Record<string, string> = {
  running: 'bg-emerald-400',
  stopped: 'bg-gray-300',
  failed: 'bg-red-400',
  crashed: 'bg-red-600',
  starting: 'bg-amber-400',
}

export function ServiceCard({ service, onAction, showToast, uptimeMode }: ServiceCardProps) {
  const [actionLoading, setActionLoading] = useState(false)
  const { t } = useI18n()

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
      showToast(t(`service.${action}ed`, { name: service.name }))
      onAction()
    } catch (err) {
      showToast(t('service.failedAction', { action, name: service.name }), 'error')
    } finally {
      setActionLoading(false)
    }
  }

  const formatUptimeElapsed = (startedAt: number) => {
    if (!startedAt) return '-'
    const seconds = Math.floor(Date.now() / 1000 - startedAt)
    if (seconds <= 0) return '-'
    const d = Math.floor(seconds / 86400)
    const h = Math.floor((seconds % 86400) / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    if (d > 0) return `${d}d ${h}h`
    if (h > 0) return `${h}h ${m}m`
    const s = seconds % 60
    return `${m}m ${s}s`
  }

  const formatStartTime = (startedAt: number) => {
    if (!startedAt) return '-'
    const d = new Date(startedAt * 1000)
    const mm = String(d.getMonth() + 1).padStart(2, '0')
    const dd = String(d.getDate()).padStart(2, '0')
    const hh = String(d.getHours()).padStart(2, '0')
    const min = String(d.getMinutes()).padStart(2, '0')
    return `${mm}-${dd} ${hh}:${min}`
  }

  const displayUptime = uptimeMode === 'start'
    ? formatStartTime(service.startedAt)
    : formatUptimeElapsed(service.startedAt)

  return (
    <div className="bg-white dark:bg-gray-800 rounded-xl shadow-sm border border-macaron-peach/60 dark:border-gray-700 p-4 hover:shadow-md transition-shadow">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <span className={`w-2.5 h-2.5 rounded-full ${statusColors[service.status] || 'bg-gray-300'}`} />
          <h3 className="font-medium text-gray-800 dark:text-gray-100">{service.name}</h3>
        </div>
        <span className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">
          {t(`service.status.${service.status}`, service.status)}
        </span>
      </div>

      <div className="grid grid-cols-3 gap-2 text-xs text-gray-500 dark:text-gray-400 mb-3">
        <div>
          <span className="block text-gray-400 dark:text-gray-500">{t('service.pid')}</span>
          <span className="text-gray-700 dark:text-gray-200">{service.pid || '-'}</span>
        </div>
        <div>
          <span className="block text-gray-400 dark:text-gray-500">{t('service.uptime')}</span>
          <span className="text-gray-700 dark:text-gray-200">{displayUptime}</span>
        </div>
        <div>
          <span className="block text-gray-400 dark:text-gray-500">{t('service.memory')}</span>
          <span className="text-gray-700 dark:text-gray-200">
            {service.memoryMB ? `${service.memoryMB} MB` : '-'}
          </span>
        </div>
      </div>

      <div className="flex gap-2">
        {service.status !== 'running' && (
          <button
            onClick={() => handleAction('start')}
            disabled={actionLoading}
            className="px-3 py-1 text-xs bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300 rounded-lg hover:bg-emerald-200 dark:hover:bg-emerald-900/60 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {t('service.start')}
          </button>
        )}
        {service.status === 'running' && (
          <button
            onClick={() => handleAction('stop')}
            disabled={actionLoading}
            className="px-3 py-1 text-xs bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 rounded-lg hover:bg-red-200 dark:hover:bg-red-900/60 transition-colors disabled:opacity-40 disabled:cursor-not-allowed inline-flex items-center gap-1"
          >
            {actionLoading && <span className="inline-block w-3 h-3 border border-red-400/30 border-t-red-600 dark:border-red-500/30 dark:border-t-red-300 rounded-full animate-spin" />}
            {t('service.stop')}
          </button>
        )}
        <button
          onClick={() => handleAction('restart')}
          disabled={actionLoading}
          className="px-3 py-1 text-xs bg-macaron-sky dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 rounded-lg hover:bg-blue-200 dark:hover:bg-blue-900/60 transition-colors disabled:opacity-40 disabled:cursor-not-allowed inline-flex items-center gap-1"
        >
          {actionLoading && <span className="inline-block w-3 h-3 border border-blue-300 dark:border-blue-500/30 border-t-blue-600 dark:border-t-blue-300 rounded-full animate-spin" />}
          {t('service.restart')}
        </button>
      </div>
    </div>
  )
}