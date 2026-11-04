import {
  createService,
  deleteService,
  fetchCronHistory,
  fetchServiceConfig,
  saveConfigToFile,
  updateServiceConfig,
  validateCronExpression,
} from '../api.js'
import { formatDuration, formatStartTime } from '../format.js'
import { i18n } from '../i18n.js'
import { selectFromQuery } from '../router.js'

const RESTART_POLICIES = ['always', 'on-failure', 'never']

const BANNER_CLASS = {
  error: 'md-banner--error',
  success: 'md-banner--success',
  info: 'md-banner--info',
}

/** Scheduler statuses, in the order the panel can meet them. */
const RUN_STATUS_KEYS = {
  success: 'config.cronStatusSuccess',
  failed: 'config.cronStatusFailed',
  overlap: 'config.cronStatusOverlap',
  started: 'config.cronStatusStarted',
}

/** Held as text while typing, parsed only when the payload is built. */
const NUMBER_KEYS = ['BACK_OFF', 'CHECK_PORT', 'CRON_TIMEOUT']

/** Every TOML key the form edits, in the order the form presents them. */
const FIELD_ORDER = [
  'WORK_DIR',
  'EXEC_CMD',
  'RESTART_POLICY',
  'BACK_OFF',
  'CHECK_PORT',
  'CRON',
  'CRON_TIMEOUT',
  'RESTART_CMD',
  'PRE_ACTION',
  'PID_FILE',
  'DEPENDS_ON',
  'ENV_VARS',
  'STDOUT',
  'STDERR',
]

const hasValue = (v) =>
  Array.isArray(v)
    ? v.length > 0
    : v && typeof v === 'object'
      ? Object.keys(v).length > 0
      : v !== '' && v !== 0 && v != null

/**
 * Low-frequency fields are collapsible. `openWhen` decides what a loaded config
 * expands, because the server always returns filled-in STDOUT/STDERR paths — the
 * logs group would otherwise never start closed.
 */
const SECTIONS = [
  {
    key: 'schedule',
    title: 'config.sectionSchedule',
    keys: ['CRON', 'CRON_TIMEOUT'],
    openWhen: (cfg) => Boolean(cfg.CRON),
  },
  {
    key: 'process',
    title: 'config.sectionProcess',
    keys: ['RESTART_CMD', 'PRE_ACTION', 'PID_FILE'],
    openWhen: (cfg) => ['RESTART_CMD', 'PRE_ACTION', 'PID_FILE'].some((k) => hasValue(cfg[k])),
  },
  {
    key: 'deps',
    title: 'config.sectionDeps',
    keys: ['DEPENDS_ON', 'ENV_VARS'],
    openWhen: (cfg) => hasValue(cfg.DEPENDS_ON) || hasValue(cfg.ENV_VARS),
  },
  {
    key: 'logs',
    title: 'config.sectionLogs',
    keys: ['STDOUT', 'STDERR'],
    openWhen: () => false,
  },
]

function blankConfig() {
  return {
    WORK_DIR: '',
    EXEC_CMD: '',
    RESTART_CMD: '',
    RESTART_POLICY: 'never',
    BACK_OFF: '',
    CHECK_PORT: '',
    PRE_ACTION: '',
    STDOUT: '',
    STDERR: '',
    DEPENDS_ON: [],
    CRON: '',
    CRON_TIMEOUT: '',
    ENV_VARS: {},
    PID_FILE: '',
  }
}

function toInt(value) {
  const n = parseInt(value, 10)
  return Number.isFinite(n) && n > 0 ? n : 0
}

/** Order-insensitive stringify, so key order in ENV_VARS is not seen as an edit. */
function canonical(value) {
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`
  if (value && typeof value === 'object') {
    return `{${Object.keys(value)
      .sort()
      .map((k) => `${JSON.stringify(k)}:${canonical(value[k])}`)
      .join(',')}}`
  }
  return JSON.stringify(value === undefined ? null : value)
}

export function configView() {
  return {
    cfg: null,
    originalCfg: null,
    selected: '',
    saving: false,
    message: null,
    messageType: 'info',
    isCreating: false,
    creatingName: '',
    cronValidating: false,
    cronResult: null,
    cronRuns: [],
    cronRunsLoading: false,
    restartPolicies: RESTART_POLICIES,
    sections: SECTIONS,
    open: {},
    envRows: [],
    envConflicts: [],

    get bannerClass() {
      return BANNER_CLASS[this.messageType]
    },

    /** Opened from a dashboard card: /config?service=<name> pre-selects it. */
    init() {
      selectFromQuery(this, (name) => this.pickService(name))
    },

    /** Existing services except this one, offered as dependency chips. */
    get depChoices() {
      return (this.$data.services || []).filter((s) => s.name !== this.selected)
    },

    get depsText() {
      return (this.cfg && this.cfg.DEPENDS_ON ? this.cfg.DEPENDS_ON : []).join(', ')
    },

    /** The server accepts a cron-only service, so either command satisfies creation. */
    get canCreate() {
      if (this.saving || !this.creatingName.trim() || !this.cfg) return false
      return Boolean(this.cfg.EXEC_CMD.trim() || this.cfg.CRON.trim())
    },

    get changed() {
      return this.changedKeys()
    },

    showMsg(msg, type = 'info') {
      this.message = msg
      this.messageType = type
    },

    /** The form as it will be sent: numbers parsed, env read from the editable rows. */
    normalized() {
      const out = { ...this.cfg }
      for (const key of NUMBER_KEYS) out[key] = toInt(out[key])
      out.DEPENDS_ON = (out.DEPENDS_ON || []).filter(Boolean)
      const env = {}
      for (const row of this.envRows) {
        const k = row.k.trim()
        if (k) env[k] = row.v
      }
      out.ENV_VARS = env
      return out
    },

    changedKeys() {
      if (!this.cfg || !this.originalCfg) return []
      const next = this.normalized()
      return FIELD_ORDER.filter((k) => canonical(next[k]) !== canonical(this.originalCfg[k]))
    },

    hasChanges() {
      return this.changedKeys().length > 0
    },

    isOpen(key) {
      return Boolean(this.open[key])
    },

    toggle(key) {
      this.open = { ...this.open, [key]: !this.open[key] }
    },

    /** Keys in a section that carry a value; listed on the header so a collapsed
     *  group still shows what it holds. */
    sectionKeys(section) {
      if (!this.cfg) return []
      return section.keys.filter((k) => hasValue(this.normalized()[k]))
    },

    sectionChanged(section) {
      return section.keys.some((k) => this.changed.includes(k))
    },

    setDeps(value) {
      if (!this.cfg) return
      this.cfg.DEPENDS_ON = value
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean)
    },

    hasDep(name) {
      return (this.cfg && this.cfg.DEPENDS_ON ? this.cfg.DEPENDS_ON : []).includes(name)
    },

    toggleDep(name) {
      if (!this.cfg) return
      const list = this.cfg.DEPENDS_ON || []
      this.cfg.DEPENDS_ON = this.hasDep(name) ? list.filter((d) => d !== name) : [...list, name]
    },

    loadEnvRows() {
      this.envRows = Object.entries((this.cfg && this.cfg.ENV_VARS) || {}).map(([k, v]) => ({ k, v }))
      this.envConflicts = []
    },

    commitEnv() {
      const seen = new Set()
      const dupes = new Set()
      for (const row of this.envRows) {
        const k = row.k.trim()
        if (!k) continue
        if (seen.has(k)) dupes.add(k)
        seen.add(k)
      }
      this.envConflicts = [...dupes]
    },

    addEnvVar() {
      let i = this.envRows.length + 1
      while (this.envRows.some((r) => r.k.trim() === `NEW_VAR_${i}`)) i += 1
      this.envRows.push({ k: `NEW_VAR_${i}`, v: '' })
      this.commitEnv()
    },

    removeEnvVar(index) {
      this.envRows.splice(index, 1)
      this.commitEnv()
    },

    async selectService(name) {
      this.selected = name
      this.cronResult = null
      this.cronRuns = []
      if (!name) {
        this.cfg = null
        this.originalCfg = null
        this.envRows = []
        this.open = {}
        return
      }
      let cfg
      try {
        cfg = await fetchServiceConfig(name)
      } catch (err) {
        // Leaving `selected` set would swallow the retry: re-picking the same
        // option fires no change event, so the form could never come back.
        this.cfg = null
        this.originalCfg = null
        this.envRows = []
        this.open = {}
        this.selected = ''
        this.showMsg(
          i18n.t('config.loadFailed', { name, message: err instanceof Error ? err.message : err }),
          'error'
        )
        return
      }
      cfg.DEPENDS_ON = cfg.DEPENDS_ON || []
      cfg.ENV_VARS = cfg.ENV_VARS || {}
      for (const key of NUMBER_KEYS) cfg[key] = cfg[key] || 0
      this.cfg = cfg
      this.originalCfg = JSON.parse(JSON.stringify(cfg))
      this.loadEnvRows()
      this.open = {}
      for (const section of SECTIONS) if (section.openWhen(cfg)) this.open[section.key] = true
      await this.loadCronRuns()
    },

    async pickService(name) {
      if (name !== this.selected && this.hasChanges() && !confirm(i18n.t('config.discardOnSwitch'))) return
      this.message = null
      this.isCreating = false
      await this.selectService(name)
    },

    discard() {
      if (!this.originalCfg) return
      this.cfg = JSON.parse(JSON.stringify(this.originalCfg))
      this.cronResult = null
      this.loadEnvRows()
      this.showMsg(i18n.t('config.changesDiscarded'), 'info')
    },

    async handleSaveAndApply() {
      if (!this.cfg || !this.selected) return
      this.saving = true
      this.message = null

      // Step 1: apply in-memory changes (this also refreshes the cron scheduler).
      const payload = this.normalized()
      if (this.hasChanges()) {
        const applyRes = await updateServiceConfig(this.selected, payload)
        if (!applyRes.success) {
          this.showMsg(applyRes.message || i18n.t('config.updateFailed'), 'error')
          this.saving = false
          return
        }
        this.originalCfg = JSON.parse(JSON.stringify(payload))
      }

      // Step 2: persist to the config file.
      const saveRes = await saveConfigToFile()
      if (!saveRes.success) {
        this.showMsg(saveRes.message || i18n.t('config.saveToFileFailed'), 'error')
      } else {
        this.showMsg(i18n.t('config.savedAndApplied'), 'success')
        // A changed schedule starts a fresh history; show it.
        await this.loadCronRuns()
      }

      this.saving = false
    },

    async handleCreate() {
      const name = this.creatingName.trim()
      if (!name) {
        this.showMsg(i18n.t('config.serviceNameRequired'), 'error')
        return
      }
      if (!this.cfg || !(this.cfg.EXEC_CMD.trim() || this.cfg.CRON.trim())) {
        this.showMsg(i18n.t('config.execCmdRequired'), 'error')
        return
      }
      this.saving = true
      this.message = null

      const res = await createService(name, this.normalized())
      if (res.success) {
        this.isCreating = false
        this.creatingName = ''
        this.selected = ''
        this.cfg = null
        this.originalCfg = null
        this.envRows = []
        this.showMsg(i18n.t('config.serviceCreated'), 'success')
        this.$data.refreshServices()
      } else {
        this.showMsg(res.message || i18n.t('config.createFailed'), 'error')
      }

      this.saving = false
    },

    async handleDelete() {
      const name = this.selected
      if (!name) return
      if (!confirm(i18n.t('config.deleteConfirm', { name }))) return
      this.saving = true
      this.message = null

      const res = await deleteService(name)
      if (res.success) {
        this.selected = ''
        this.cfg = null
        this.originalCfg = null
        this.envRows = []
        this.showMsg(i18n.t('config.serviceDeleted'), 'success')
        this.$data.refreshServices()
      } else {
        this.showMsg(res.message || i18n.t('config.deleteFailed'), 'error')
      }

      this.saving = false
    },

    async handleValidateCron() {
      const expr = this.cfg && this.cfg.CRON
      if (!expr) return
      this.cronValidating = true
      this.cronResult = null
      try {
        this.cronResult = await validateCronExpression(expr)
      } catch (err) {
        this.cronResult = {
          valid: false,
          message: err instanceof Error ? err.message : i18n.t('config.networkError'),
        }
      } finally {
        this.cronValidating = false
      }
    },

    startCreate() {
      this.selected = ''
      this.creatingName = ''
      this.cfg = blankConfig()
      this.originalCfg = null
      this.isCreating = true
      this.message = null
      this.envRows = []
      this.envConflicts = []
      this.cronResult = null
      this.cronRuns = []
      this.open = {}
    },

    cancelCreate() {
      this.isCreating = false
      this.creatingName = ''
      this.selected = ''
      this.cfg = null
      this.originalCfg = null
      this.envRows = []
      this.cronRuns = []
      this.open = {}
      this.message = null
    },

    cronBannerText() {
      if (!this.cronResult) return ''
      if (this.cronResult.valid) {
        return i18n.t('config.cronValid', {
          next1: this.cronResult.nextRun || '',
          next2: this.cronResult.nextRun2 || '',
        })
      }
      return i18n.t('config.cronInvalid', { message: this.cronResult.message })
    },

    /** Recent runs of the selected schedule, newest first. */
    async loadCronRuns() {
      this.cronRuns = []
      const cron = this.cfg && String(this.cfg.CRON || '').trim()
      if (!this.selected || this.isCreating || !cron) return
      this.cronRunsLoading = true
      try {
        const runs = await fetchCronHistory(this.selected)
        this.cronRuns = [...runs].reverse()
      } catch {
        // A history that cannot be read is not an error worth a banner: the
        // panel just stays empty and the refresh button invites a retry.
        this.cronRuns = []
      } finally {
        this.cronRunsLoading = false
      }
    },

    runChipClass(run) {
      if (run.status === 'success') return 'md-chip--success'
      if (run.status === 'failed') return 'md-chip--error'
      if (run.status === 'overlap') return 'md-chip--plain'
      return 'md-chip--filled'
    },

    runStatus(run) {
      const key = RUN_STATUS_KEYS[run.status]
      return key ? i18n.t(key) : run.status
    },

    runTime(run) {
      return formatStartTime(run.startedAt)
    },

    /** Exit code and duration, or "still running" for a run with no end yet. */
    runDetail(run) {
      const parts = []
      if (run.status === 'started') {
        parts.push(i18n.t('config.cronRunRunning'))
      } else if (run.endedAt) {
        parts.push(formatDuration(run.endedAt - run.startedAt))
      }
      if (run.exitCode !== undefined && run.exitCode !== null) {
        parts.push(`${i18n.t('config.cronRunExit')} ${run.exitCode}`)
      }
      return parts.join(' · ')
    },

    get cronBannerClass() {
      const ok = this.cronResult && this.cronResult.valid
      return `md-banner md-banner--compact ${ok ? 'md-banner--success' : 'md-banner--error'}`
    },
  }
}
