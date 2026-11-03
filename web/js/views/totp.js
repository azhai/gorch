import {
  totpDisable,
  totpRegenerateBackupCodes,
  totpSetup,
  totpStatus,
  totpVerifySetup,
} from '../api.js'
import { i18n } from '../i18n.js'

const BLANK_STATUS = { enabled: false, backupCodes: 0, hasBinding: false }

export function totpView() {
  return {
    status: null,
    setupData: null,
    verifyCode: '',
    loading: false,
    error: '',
    success: '',

    start() {
      this.loadStatus()
    },

    async loadStatus() {
      this.loading = true
      try {
        const res = await totpStatus()
        this.status = res.success && res.data ? res.data : BLANK_STATUS
      } catch {
        this.error = i18n.t('totp.failedLoad')
      } finally {
        this.loading = false
      }
    },

    async handleSetup() {
      this.loading = true
      this.error = ''
      this.success = ''
      try {
        const res = await totpSetup()
        if (res.success && res.data) {
          this.setupData = res.data
        } else {
          this.error = res.message || i18n.t('totp.setupFailed')
        }
      } catch {
        this.error = i18n.t('totp.failedSetup')
      } finally {
        this.loading = false
      }
    },

    async handleVerifySetup() {
      if (!this.verifyCode) {
        this.error = i18n.t('totp.enterCode')
        return
      }
      this.loading = true
      this.error = ''
      try {
        const res = await totpVerifySetup(this.verifyCode)
        if (res.success) {
          this.success = i18n.t('totp.enabledSuccess')
          this.setupData = null
          this.verifyCode = ''
          await this.loadStatus()
        } else {
          this.error = res.message || i18n.t('totp.verifyFailed')
        }
      } catch {
        this.error = i18n.t('totp.failedVerify')
      } finally {
        this.loading = false
      }
    },

    async handleDisable() {
      if (!confirm(i18n.t('totp.disableConfirm'))) return
      this.loading = true
      this.error = ''
      try {
        const res = await totpDisable()
        if (res.success) {
          this.success = i18n.t('totp.disabledSuccess')
          await this.loadStatus()
        } else {
          this.error = res.message || i18n.t('totp.failedDisable')
        }
      } catch {
        this.error = i18n.t('totp.failedDisable')
      } finally {
        this.loading = false
      }
    },

    async handleRegenerateBackupCodes() {
      if (!confirm(i18n.t('totp.regenerateConfirm'))) return
      this.loading = true
      this.error = ''
      try {
        const res = await totpRegenerateBackupCodes()
        if (res.success && res.data) {
          if (this.setupData) {
            this.setupData = { ...this.setupData, backupCodes: res.data.backupCodes }
          }
          this.success = i18n.t('totp.regenerated')
          await this.loadStatus()
        } else {
          this.error = res.message || i18n.t('totp.failedRegenerate')
        }
      } catch {
        this.error = i18n.t('totp.failedRegenerate')
      } finally {
        this.loading = false
      }
    },

    setVerifyCode(value) {
      this.verifyCode = value.replace(/\D/g, '').slice(0, 6)
    },

    cancelSetup() {
      this.setupData = null
      this.verifyCode = ''
    },
  }
}
