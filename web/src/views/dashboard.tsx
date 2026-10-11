import m from 'mithril'
import { ServiceStatus, fetchServices } from '../api/client'
import { ServiceCard, type UptimeMode } from '../components/serviceCard'
import { i18n } from '../state/i18n'
import { onSSE, setSSEInterval, sse, type SSEMessage } from '../state/sse'

const SSE_INTERVAL_OPTIONS = [
  { value: '5s', label: '5s' },
  { value: '1m', label: '1m' },
  { value: '15m', label: '15m' },
]

interface StatusChangePayload {
  name: string
  status: string
  pid?: number
  memoryMB: number
  startedAt?: number
  exitCode?: number
}

interface UptimeTickPayload {
  services: Record<string, { pid: number; memoryMB: number; startedAt?: number }>
}


interface DashboardState {
  services: ServiceStatus[]
  uptimeMode: UptimeMode
  tick: number
}

// Closure-scoped state: Mithril components have no `this` typing for helper
// methods, so each view keeps its own plain object.
export const Dashboard: m.Component = (() => {
  const state: DashboardState = { services: [], uptimeMode: 'elapsed', tick: 0 }
  let unsubscribe = () => {}
  let ticker = 0

  async function loadServices(): Promise<void> {
    try {
      state.services = await fetchServices()
    } catch (err) {
      // A rejected AuthError means the shell is about to redirect to /login.
      if (err instanceof Error && err.name === 'AuthError') return
    }
    m.redraw()
  }

  function applyMessage(message: SSEMessage): void {
    if (message.type === 'status_change') {
      const p = message.payload as StatusChangePayload
      state.services = state.services.map((svc) =>
        svc.name !== p.name
          ? svc
          : {
              ...svc,
              status: p.status,
              pid: p.pid || 0,
              memoryMB: p.memoryMB,
              startedAt: p.startedAt ?? svc.startedAt,
              exitCode: p.exitCode ?? svc.exitCode,
            }
      )
    }

    if (message.type === 'uptime_tick') {
      const p = message.payload as UptimeTickPayload
      state.services = state.services.map((svc) => {
        const info = p.services[svc.name]
        if (info !== undefined) {
          return { ...svc, pid: info.pid, memoryMB: info.memoryMB, startedAt: info.startedAt ?? svc.startedAt }
        }
        // The server only reports running services. When one is absent it has no
        // live process - reset startedAt so the elapsed timer stops counting.
        return svc.startedAt === 0 ? svc : { ...svc, startedAt: 0 }
      })
    }

    m.redraw()
  }

  return {
    oninit() {
      loadServices()
      unsubscribe = onSSE((message) => {
        if (message) applyMessage(message)
      })
      ticker = window.setInterval(() => {
        state.tick++
      }, 1000)
    },

    onremove() {
      unsubscribe()
      window.clearInterval(ticker)
    },

    view() {
      const services = state.services

      return (
        <div>
          <div class="md-row md-row--between" style="margin-bottom:16px">
            <h2>{i18n.t('dashboard.services')}</h2>
            <div class="md-row md-row--tight">
              <div class="md-row md-row--tight">
                <span class="md-muted-sm">{i18n.t('dashboard.duration')}</span>
                <select
                  id="sse-interval"
                  class="md-select md-select--sm"
                  value={sse.interval}
                  onchange={(e: Event) => {
                    setSSEInterval((e.target as HTMLSelectElement).value)
                    m.redraw()
                  }}
                >
                  {SSE_INTERVAL_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </select>
              </div>
              <div class="md-row md-row--tight">
                <span class="md-muted-sm">{i18n.t('dashboard.uptime')}</span>
                <select
                  id="uptime-mode"
                  class="md-select md-select--sm"
                  value={state.uptimeMode}
                  onchange={(e: Event) => {
                    state.uptimeMode = (e.target as HTMLSelectElement).value as UptimeMode
                  }}
                >
                  <option value="elapsed">{i18n.t('dashboard.elapsed')}</option>
                  <option value="start">{i18n.t('dashboard.startTime')}</option>
                </select>
              </div>
            </div>
          </div>

          {services.length > 0 && (
            <div class="md-card-grid">
              {services.map((svc) =>
                m(ServiceCard, {
                  key: svc.name,
                  service: svc,
                  uptimeMode: state.uptimeMode,
                  tick: state.tick,
                  onAction: loadServices,
                })
              )}
            </div>
          )}

          {services.length === 0 && <div class="md-empty">{i18n.t('dashboard.noServices')}</div>}
        </div>
      )
    },
  }
})()
