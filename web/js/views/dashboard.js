import { restartService, startService, stopService } from '../api.js'
import { showToast } from '../toast.js'
import { STATUS_CLASS, formatUptime } from '../format.js'

const SSE_OPTIONS = ['5s', '1m', '15m']

const DONE_KEY = { start: 'started', stop: 'stopped', restart: 'restarted' }

const ACTIONS = {
  start: startService,
  stop: stopService,
  restart: restartService,
}

function runAction(action, name) {
  const call = ACTIONS[action]
  return call ? call(name) : Promise.reject(new Error(`unknown action: ${action}`))
}

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

    /** Card links carry the service in the query so the target page can select it. */
    configPath(name) {
      return `/config?service=${encodeURIComponent(name)}`
    },

    logsPath(name) {
      return `/logs?service=${encodeURIComponent(name)}`
    },

    /** Start/Stop/Restart: apply the state the server answered with, then keep
     *  the card in sync until the transition finishes. The button stays busy
     *  for that whole window, so a stop that has to wait out SIGTERM does not
     *  look like a click that was ignored. */
    async act(action, name) {
      this.pending[name] = true
      try {
        const result = await runAction(action, name)
        if (!result.success) {
          showToast(result.message || this.$data.t('service.failedAction', { action, name }), 'error')
          return
        }

        showToast(this.$data.t(`service.${DONE_KEY[action]}`, { name }))
        this.$data.applyStatus(result.data)
        await this.$data.settleService(name)
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
