import {
  createService,
  deleteService,
  fetchServiceConfig,
  saveConfigToFile,
  updateServiceConfig,
  validateCronExpression,
} from '../api.js'
import { i18n } from '../i18n.js'

const RESTART_POLICIES = [
  { value: 'always', label: 'always' },
  { value: 'on-failure', label: 'on-failure' },
  { value: 'never', label: 'never' },
]

const BANNER_CLASS = {
  error: 'md-banner--error',
  success: 'md-banner--success',
  info: 'md-banner--info',
}

function blankConfig() {
  return {
    WORK_DIR: '.',
    EXEC_CMD: '',
    RESTART_CMD: '',
    RESTART_POLICY: 'never',
    BACK_OFF: 0,
    CHECK_PORT: 0,
    PRE_ACTION: '',
    STDOUT: '',
    STDERR: '',
    DEPENDS_ON: [],
    CRON: '',
    ENV_VARS: {},
    PID_FILE: '',
  }
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
    restartPolicies: RESTART_POLICIES,

    get envEntries() {
      if (!this.cfg) return []
      return Object.entries(this.cfg.ENV_VARS || {}).map(([k, v]) => ({ k, v }))
    },

    get bannerClass() {
      return BANNER_CLASS[this.messageType]
    },

    get depsText() {
      return (this.cfg && this.cfg.DEPENDS_ON ? this.cfg.DEPENDS_ON : []).join(', ')
    },

    get canCreate() {
      return Boolean(this.creatingName.trim()) && Boolean(this.cfg && this.cfg.EXEC_CMD.trim()) && !this.saving
    },

    showMsg(msg, type = 'info') {
      this.message = msg
      this.messageType = type
    },

    hasChanges() {
      return Boolean(
        this.cfg && this.originalCfg && JSON.stringify(this.cfg) !== JSON.stringify(this.originalCfg)
      )
    },

    setField(key, value) {
      if (!this.cfg) return
      this.cfg[key] = value
      if (key === 'CRON') this.cronResult = null
    },

    setNumber(key, value) {
      this.setField(key, parseInt(value, 10) || 0)
    },

    setDeps(value) {
      this.setField(
        'DEPENDS_ON',
        value
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean)
      )
    },

    renameEnvVar(oldKey, newKey, value) {
      if (!this.cfg) return
      const envs = this.cfg.ENV_VARS || {}
      if (newKey !== oldKey && envs[newKey] !== undefined) return
      const next = { ...envs }
      delete next[oldKey]
      next[newKey] = value
      this.cfg.ENV_VARS = next
    },

    addEnvVar() {
      if (!this.cfg) return
      const envs = this.cfg.ENV_VARS || {}
      const base = 'NEW_VAR'
      let name = base
      let i = 1
      while (envs[name] !== undefined) name = `${base}_${i++}`
      this.cfg.ENV_VARS = { ...envs, [name]: '' }
    },

    removeEnvVar(key) {
      if (!this.cfg) return
      const next = { ...this.cfg.ENV_VARS }
      delete next[key]
      this.cfg.ENV_VARS = next
    },

    async selectService(name) {
      this.selected = name
      this.cronResult = null
      if (!name) {
        this.cfg = null
        this.originalCfg = null
        return
      }
      const cfg = await fetchServiceConfig(name)
      cfg.DEPENDS_ON = cfg.DEPENDS_ON || []
      cfg.ENV_VARS = cfg.ENV_VARS || {}
      this.cfg = cfg
      this.originalCfg = JSON.parse(JSON.stringify(cfg))
    },

    pickService(name) {
      this.message = null
      this.isCreating = false
      this.selectService(name)
    },

    async handleSaveAndApply() {
      if (!this.cfg || !this.selected) return
      this.saving = true
      this.message = null

      // Step 1: apply in-memory changes (this also refreshes the cron scheduler).
      if (this.hasChanges()) {
        const applyRes = await updateServiceConfig(this.selected, this.cfg)
        if (!applyRes.success) {
          this.showMsg(applyRes.message || i18n.t('config.updateFailed'), 'error')
          this.saving = false
          return
        }
        this.originalCfg = JSON.parse(JSON.stringify(this.cfg))
      }

      // Step 2: persist to the config file.
      const saveRes = await saveConfigToFile()
      if (!saveRes.success) {
        this.showMsg(saveRes.message || i18n.t('config.saveToFileFailed'), 'error')
      } else {
        this.showMsg(i18n.t('config.savedAndApplied'), 'success')
      }

      this.saving = false
    },

    async handleCreate() {
      const name = this.creatingName.trim()
      if (!name) {
        this.showMsg(i18n.t('config.serviceNameRequired'), 'error')
        return
      }
      if (!this.cfg || !this.cfg.EXEC_CMD.trim()) {
        this.showMsg(i18n.t('config.execCmdRequired'), 'error')
        return
      }
      this.saving = true
      this.message = null

      const res = await createService(name, this.cfg)
      if (res.success) {
        this.isCreating = false
        this.creatingName = ''
        this.selected = ''
        this.cfg = null
        this.originalCfg = null
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
    },

    cancelCreate() {
      this.isCreating = false
      this.creatingName = ''
      this.selected = ''
      this.cfg = null
      this.originalCfg = null
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

    get cronBannerClass() {
      const ok = this.cronResult && this.cronResult.valid
      return `md-banner md-banner--compact ${ok ? 'md-banner--success' : 'md-banner--error'}`
    },
  }
}
