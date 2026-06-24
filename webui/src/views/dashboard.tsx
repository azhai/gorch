import { useState, useEffect, useCallback, useRef } from 'react'
import { fetchServices, ServiceStatus } from '../api/client'
import { ServiceCard } from '../components/serviceCard'
import { Toast, useToast } from '../components/toast'
import { useSSE } from '../hooks/useSSE'

export default function Dashboard() {
  const [services, setServices] = useState<ServiceStatus[]>([])
  const { showToast, toast } = useToast()
  const { lastMessage } = useSSE()
  const lastServerUptime = useRef<Record<string, number>>({})

  const loadServices = useCallback(async () => {
    try {
      const data = await fetchServices()
      setServices(data)
      const baseline: Record<string, number> = {}
      for (const s of data) {
        if (s.status === 'running' && s.uptime > 0) {
          baseline[s.name] = s.uptime
        }
      }
      lastServerUptime.current = baseline
    } catch (err) {
      if (err instanceof Error && err.name === 'AuthError') return
    }
  }, [])

  useEffect(() => {
    loadServices()
  }, [loadServices])

  // handle WS messages
  useEffect(() => {
    if (!lastMessage) return

    if (lastMessage.type === 'status_change') {
      const payload = lastMessage.payload as {
        name: string
        status: string
        pid?: number
        uptime?: number
        memoryMB: number
      }
      setServices((prev) =>
        prev.map((s) => {
          if (s.name !== payload.name) return s
          const updated = { ...s, status: payload.status, pid: payload.pid || 0, memoryMB: payload.memoryMB }
          if (payload.uptime !== undefined) {
            updated.uptime = payload.uptime
            lastServerUptime.current[payload.name] = payload.uptime
          }
          if (payload.status !== 'running') {
            delete lastServerUptime.current[payload.name]
          }
          return updated
        })
      )
    }

    if (lastMessage.type === 'uptime_tick') {
      const payload = lastMessage.payload as { services: Record<string, { pid: number; uptime: number; memoryMB: number }> }
      lastServerUptime.current = { ...Object.fromEntries(Object.entries(payload.services).map(([k, v]) => [k, v.uptime])) }
      setServices((prev) =>
        prev.map((s) => {
          const info = payload.services[s.name]
          if (info !== undefined) {
            return { ...s, pid: info.pid, uptime: info.uptime, memoryMB: info.memoryMB }
          }
          return s
        })
      )
    }
  }, [lastMessage])

  // local interpolation: increment uptime every second
  useEffect(() => {
    const timer = setInterval(() => {
      setServices((prev) =>
        prev.map((s) => {
          if (s.status === 'running') {
            return { ...s, uptime: (s.uptime || 0) + 1 }
          }
          return s
        })
      )
    }, 1000)
    return () => clearInterval(timer)
  }, [])

  return (
    <div>
      <h2 className="text-lg font-semibold text-gray-800 mb-4">Services</h2>
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {services.map((svc) => (
          <ServiceCard
            key={svc.name}
            service={svc}
            onAction={loadServices}
            showToast={showToast}
          />
        ))}
      </div>

      {services.length === 0 && (
        <div className="text-center py-12 text-gray-400">
          No services configured
        </div>
      )}

      {toast && <Toast message={toast.message} type={toast.type} onClose={() => {}} />}
    </div>
  )
}
