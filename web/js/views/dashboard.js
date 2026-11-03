import { restartService, startService, stopService } from '../api.js'
import { showToast } from '../toast.js'
import { STATUS_CLASS, formatUptime } from '../format.js'

const SSE_OPTIONS = ['5s', '1m', '15m']

const DONE_KEY = { start: 'started', stop: 'stopped', restart: 'restarted' }

export function dashboardView() {
  return {
    uptimeMode: 'elapsed',
    sseOptions: SSE_OPTIONS,
    pending: {},

    statusClass(status) {
      return STATUS_CLASS[status] ?? ''
    },

    // Root-component members (services/now/t/refreshServices) come from the enclosing
    // x-data scope; `$data` is the merged scope, so reads stay reactive.
    uptimeFor(service) {
      return formatUptime(service.startedAt, this.uptimeMode, this.$data.now)
    },

    memoryFor(service) {
      return service.memoryMB ? `${service.memoryMB} MB` : '-'
    },

    statusLabel(service) {
      return this.$data.t(`service.status.${service.status}`, service.status)
    },

    async act(action, name) {
      this.pending[name] = true
      try {
        if (action === 'start') await startService(name)
        else if (action === 'stop') await stopService(name)
        else await restartService(name)

        showToast(this.$data.t(`service.${DONE_KEY[action]}`, { name }))
        await this.$data.refreshServices()
      } catch {
        showToast(this.$data.t('service.failedAction', { action, name }), 'error')
      } finally {
        this.pending[name] = false
      }
    },

    isPending(name) {
      return Boolean(this.pending[name])
    },
  }
}
