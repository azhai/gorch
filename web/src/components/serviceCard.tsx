import m from 'mithril'
import { ServiceStatus, restartService, startService, stopService } from '../api/client'
import { i18n } from '../state/i18n'
import { showToast } from '../state/toast'

type Action = 'start' | 'stop' | 'restart'
export type UptimeMode = 'elapsed' | 'start'

export interface ServiceCardAttrs {
  service: ServiceStatus
  onAction: () => void
  uptimeMode: UptimeMode
  /** Bumped once a second by the dashboard to tick the elapsed-time display. */
  tick: number
}

const STATUS_CLASS: Record<string, string> = {
  running: 'md-status-dot--running',
  stopped: 'md-status-dot--stopped',
  failed: 'md-status-dot--failed',
  crashed: 'md-status-dot--crashed',
  starting: 'md-status-dot--starting',
}

// Per-card in-flight flag. A WeakMap keyed by the vnode keeps the state scoped
// to the component instance without relying on `this`.
const inFlight = new WeakMap<object, boolean>()

function formatUptimeElapsed(startedAt: number): string {
  if (!startedAt) return '-'
  const seconds = Math.floor(Date.now() / 1000 - startedAt)
  if (seconds <= 0) return '-'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m ${seconds % 60}s`
}

function formatStartTime(startedAt: number): string {
  if (!startedAt) return '-'
  const d = new Date(startedAt * 1000)
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  const hh = String(d.getHours()).padStart(2, '0')
  const min = String(d.getMinutes()).padStart(2, '0')
  return `${mm}-${dd} ${hh}:${min}`
}

async function handleAction(vnode: m.Vnode<ServiceCardAttrs>, action: Action, name: string) {
  inFlight.set(vnode, true)
  try {
    if (action === 'start') await startService(name)
    else if (action === 'stop') await stopService(name)
    else await restartService(name)

    showToast(i18n.t(`service.${action}ed`, { name }))
    vnode.attrs.onAction()
  } catch {
    showToast(i18n.t('service.failedAction', { action, name }), 'error')
  } finally {
    inFlight.set(vnode, false)
    m.redraw()
  }
}

export const ServiceCard: m.Component<ServiceCardAttrs> = {
  view(vnode) {
    const { service, uptimeMode } = vnode.attrs
    const loading = inFlight.get(vnode) ?? false
    const uptime =
      uptimeMode === 'start'
        ? formatStartTime(service.startedAt)
        : formatUptimeElapsed(service.startedAt)

    return (
      <div class="md-card md-card--elevated">
        <div class="md-service-card__head">
          <div class="md-service-card__name">
            <span class={`md-status-dot ${STATUS_CLASS[service.status] ?? ''}`} />
            <h3>{service.name}</h3>
          </div>
          <span class="md-chip">{i18n.t(`service.status.${service.status}`, service.status)}</span>
        </div>

        <div class="md-metric-grid">
          <div>
            <span class="md-metric-label">{i18n.t('service.pid')}</span>
            <span class="md-metric-value">{service.pid || '-'}</span>
          </div>
          <div>
            <span class="md-metric-label">{i18n.t('service.uptime')}</span>
            <span class="md-metric-value">{uptime}</span>
          </div>
          <div>
            <span class="md-metric-label">{i18n.t('service.memory')}</span>
            <span class="md-metric-value">{service.memoryMB ? `${service.memoryMB} MB` : '-'}</span>
          </div>
        </div>

        <div class="md-row md-row--tight">
          {service.status !== 'running' && (
            <button
              class="md-btn md-btn--tonal md-btn--sm"
              disabled={loading}
              onclick={() => handleAction(vnode, 'start', service.name)}
            >
              {i18n.t('service.start')}
            </button>
          )}
          {service.status === 'running' && (
            <button
              class="md-btn md-btn--danger md-btn--sm"
              disabled={loading}
              onclick={() => handleAction(vnode, 'stop', service.name)}
            >
              {loading && <span class="md-spinner" />}
              {i18n.t('service.stop')}
            </button>
          )}
          <button
            class="md-btn md-btn--outlined md-btn--sm"
            disabled={loading}
            onclick={() => handleAction(vnode, 'restart', service.name)}
          >
            {loading && <span class="md-spinner" />}
            {i18n.t('service.restart')}
          </button>
        </div>
      </div>
    )
  },
}
