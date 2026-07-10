import { useState, useEffect, useCallback } from 'react'
import { fetchServices, ServiceStatus } from '../api/client'
import { ServiceCard } from '../components/serviceCard'
import { Toast, useToast } from '../components/toast'
import { useSSE } from '../hooks/useSSE'
import { useI18n } from '../i18n/I18nProvider'

const SSE_INTERVAL_OPTIONS = [
  { value: '5s', label: '5s' },
  { value: '1m', label: '1m' },
  { value: '15m', label: '15m' },
]

export default function Dashboard() {
  const [services, setServices] = useState<ServiceStatus[]>([])
  const [sseInterval, setSseInterval] = useState('5s')
  const [uptimeMode, setUptimeMode] = useState<'elapsed' | 'start'>('elapsed')
  const [, setTick] = useState(0)
  const { showToast, toast } = useToast()
  const { lastMessage } = useSSE(sseInterval)
  const { t } = useI18n()

  const loadServices = useCallback(async () => {
    try {
      const data = await fetchServices()
      setServices(data)
    } catch (err) {
      if (err instanceof Error && err.name === 'AuthError') return
    }
  }, [])

  useEffect(() => {
    loadServices()
  }, [loadServices])

  useEffect(() => {
    if (!lastMessage) return

    if (lastMessage.type === 'status_change') {
      const payload = lastMessage.payload as {
        name: string
        status: string
        pid?: number
        memoryMB: number
        startedAt?: number
        exitCode?: number
      }
      setServices((prev) =>
        prev.map((s) => {
          if (s.name !== payload.name) return s
          return {
            ...s,
            status: payload.status,
            pid: payload.pid || 0,
            memoryMB: payload.memoryMB,
            startedAt: payload.startedAt ?? s.startedAt,
            exitCode: payload.exitCode ?? s.exitCode,
          }
        })
      )
    }

    if (lastMessage.type === 'uptime_tick') {
      const payload = lastMessage.payload as {
        services: Record<string, { pid: number; memoryMB: number; startedAt?: number }>
      }
      setServices((prev) =>
        prev.map((s) => {
          const info = payload.services[s.name]
          if (info !== undefined) {
            return {
              ...s,
              pid: info.pid,
              memoryMB: info.memoryMB,
              startedAt: info.startedAt ?? s.startedAt,
            }
          }
          return s
        })
      )
    }
  }, [lastMessage])

  useEffect(() => {
    if (uptimeMode !== 'elapsed') return
    const timer = setInterval(() => {
      setTick((t) => t + 1)
    }, 1000)
    return () => clearInterval(timer)
  }, [uptimeMode])

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
        <h2 className="text-lg font-semibold text-gray-800 dark:text-gray-100">{t('dashboard.services')}</h2>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <label htmlFor="sse-interval" className="text-xs text-gray-500 dark:text-gray-400 whitespace-nowrap">
              {t('dashboard.duration')}
            </label>
            <select
              id="sse-interval"
              value={sseInterval}
              onChange={(e) => setSseInterval(e.target.value)}
              className="text-xs border border-gray-200 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200 rounded-lg px-2 py-1 bg-white focus:outline-none focus:ring-1 focus:ring-macaron-peach"
            >
              {SSE_INTERVAL_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
          </div>
          <div className="flex items-center gap-2">
            <label htmlFor="uptime-mode" className="text-xs text-gray-500 dark:text-gray-400 whitespace-nowrap">
              {t('dashboard.uptime')}
            </label>
            <select
              id="uptime-mode"
              value={uptimeMode}
              onChange={(e) => setUptimeMode(e.target.value as 'elapsed' | 'start')}
              className="text-xs border border-gray-200 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200 rounded-lg px-2 py-1 bg-white focus:outline-none focus:ring-1 focus:ring-macaron-peach"
            >
              <option value="elapsed">{t('dashboard.elapsed')}</option>
              <option value="start">{t('dashboard.startTime')}</option>
            </select>
          </div>
        </div>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {services.map((svc) => (
          <ServiceCard
            key={svc.name}
            service={svc}
            onAction={loadServices}
            showToast={showToast}
            uptimeMode={uptimeMode}
          />
        ))}
      </div>

      {services.length === 0 && (
        <div className="text-center py-12 text-gray-400 dark:text-gray-500">
          {t('dashboard.noServices')}
        </div>
      )}

      {toast && <Toast message={toast.message} type={toast.type} onClose={() => {}} />}
    </div>
  )
}
