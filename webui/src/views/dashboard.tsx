import { useState, useEffect, useCallback, useRef } from 'react'
import { fetchServices, ServiceStatus } from '../api/client'
import { ServiceCard } from '../components/serviceCard'
import { Toast, useToast } from '../components/toast'
import { Dialog, useDialog } from '../components/dialog'
import { useSSE } from '../hooks/useSSE'

export default function Dashboard() {
  const [services, setServices] = useState<ServiceStatus[]>([])
  const { showToast, toast } = useToast()
  const { dialog, showDialog } = useDialog()
  const { lastMessage } = useSSE()
  const lastServerUptime = useRef<Record<string, number>>({})

  const loadServices = useCallback(async () => {
    try {
      const data = await fetchServices()
      setServices(data)
      // store server uptime as baseline
      const baseline: Record<string, number> = {}
      for (const s of data) {
        if (s.status === 'running' && s.uptime > 0) {
          baseline[s.name] = s.uptime
        }
      }
      lastServerUptime.current = baseline
    } catch (err) {
      if (err instanceof Error && err.name === 'AuthError') return
      showDialog('Load Failed', String(err))
    }
  }, [showDialog])

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
        restartCount: number
      }
      setServices((prev) =>
        prev.map((s) => {
          if (s.name !== payload.name) return s
          const updated = { ...s, status: payload.status, pid: payload.pid || 0, restartCount: payload.restartCount }
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
      const payload = lastMessage.payload as { services: Record<string, number> }
      lastServerUptime.current = { ...lastServerUptime.current, ...payload.services }
      setServices((prev) =>
        prev.map((s) => {
          const serverUptime = payload.services[s.name]
          if (serverUptime !== undefined) {
            return { ...s, uptime: serverUptime }
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
          if (s.status === 'running' && s.uptime > 0) {
            return { ...s, uptime: s.uptime + 1 }
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
      {dialog && <Dialog title={dialog.title} message={dialog.message} onClose={() => {}} />}
    </div>
  )
}
