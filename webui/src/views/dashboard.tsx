import { useState, useEffect, useCallback } from 'react'
import { fetchServices, ServiceStatus } from '../api/client'
import { ServiceCard } from '../components/serviceCard'
import { Toast, useToast } from '../components/toast'
import { Dialog, useDialog } from '../components/dialog'
import { useWs } from '../hooks/useWs'

export default function Dashboard() {
  const [services, setServices] = useState<ServiceStatus[]>([])
  const { showToast, toast } = useToast()
  const { dialog, showDialog } = useDialog()
  const { lastMessage } = useWs()

  const loadServices = useCallback(async () => {
    try {
      const data = await fetchServices()
      setServices(data)
    } catch (err) {
      showDialog('Load Failed', String(err))
    }
  }, [showDialog])

  useEffect(() => {
    loadServices()
  }, [loadServices])

  useEffect(() => {
    if (!lastMessage || lastMessage.type !== 'status_change') return
    const payload = lastMessage.payload as {
      name: string
      status: string
      pid?: number
      restartCount: number
    }
    setServices((prev) =>
      prev.map((s) =>
        s.name === payload.name
          ? { ...s, status: payload.status, pid: payload.pid || 0, restartCount: payload.restartCount }
          : s
      )
    )
  }, [lastMessage])

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