import { login, loginTotp } from '../api.js'
import { setToken } from '../auth.js'
import { i18n } from '../i18n.js'
import { navigate } from '../router.js'

export function loginView() {
  return {
    username: '',
    password: '',
    totpCode: '',
    step: 'credentials',
    loading: false,
    error: null,

    async submitCredentials() {
      if (!this.username.trim() || !this.password.trim()) {
        this.error = i18n.t('login.enterCredentials')
        return
      }

      this.loading = true
      this.error = null

      try {
        const { ok, status, json } = await login(this.username, this.password)

        if (ok && json.success) {
          if (json.data && json.data.requireTotp) {
            this.step = 'totp'
          } else if (json.data && json.data.token) {
            setToken(json.data.token)
            this.$data.syncAuth()
            navigate('/')
          }
        } else if (status === 401) {
          this.error = i18n.t('login.invalidCredentials')
        } else {
          this.error = json.message || i18n.t('login.networkError')
        }
      } catch {
        this.error = i18n.t('login.networkError')
      } finally {
        this.loading = false
      }
    },

    async submitTotp() {
      if (this.totpCode.length !== 6) {
        this.error = i18n.t('login.enter6DigitCode')
        return
      }

      this.loading = true
      this.error = null

      try {
        const { ok, json } = await loginTotp(this.username, this.totpCode)

        if (ok && json.success && json.data && json.data.token) {
          setToken(json.data.token)
          this.$data.syncAuth()
          navigate('/')
        } else {
          this.error = json.message || i18n.t('login.invalidCode')
        }
      } catch {
        this.error = i18n.t('login.networkError')
      } finally {
        this.loading = false
      }
    },

    back() {
      this.step = 'credentials'
      this.totpCode = ''
      this.error = null
    },

    onlyDigits(field, value) {
      this[field] = value.replace(/\D/g, '').slice(0, 6)
    },
  }
}
