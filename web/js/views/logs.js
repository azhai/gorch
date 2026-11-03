import { clearLogs, fetchLogs } from '../api.js'
import { i18n } from '../i18n.js'

const LOG_TYPES = ['stdout', 'stderr']

export function logsView() {
  return {
    logTypes: LOG_TYPES,
    selected: '',
    logType: 'stdout',
    logData: null,
    clearing: false,
    error: null,

    get lines() {
      return this.logData ? this.logData.lines : []
    },

    async pick(name) {
      this.selected = name
      this.error = null
      await this.loadLogs()
    },

    async pickType(type) {
      this.logType = type
      await this.loadLogs()
    },

    async loadLogs() {
      if (!this.selected) {
        this.logData = null
        return
      }
      try {
        this.logData = await fetchLogs(this.selected, 500, this.logType)
      } catch (err) {
        this.error = err instanceof Error ? err.message : String(err)
      }
    },

    async clear() {
      if (!this.selected) return
      this.clearing = true
      this.error = null
      try {
        await clearLogs(this.selected, this.logType)
        this.logData = this.logData ? { ...this.logData, lines: [] } : null
      } catch {
        this.error = i18n.t('logs.failedClear')
      } finally {
        this.clearing = false
      }
    },
  }
}
