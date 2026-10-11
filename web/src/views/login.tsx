import m from 'mithril'
import { API_BASE } from '../api/config'
import { getToken, isAuthEnabled, setToken } from '../api/authStore'
import { i18n, type Lang } from '../state/i18n'
import { theme, type ThemeOption } from '../state/theme'

const LANG_OPTIONS: { value: Lang; label: string }[] = [
  { value: 'en', label: 'English' },
  { value: 'zh-CN', label: '简体中文' },
]

const THEME_OPTIONS: { value: ThemeOption; labelKey: string }[] = [
  { value: 'orange', labelKey: 'theme.orange' },
  { value: 'mint', labelKey: 'theme.mint' },
  { value: 'pinkblue', labelKey: 'theme.pinkblue' },
  { value: 'monet', labelKey: 'theme.monet' },
  { value: 'dark', labelKey: 'theme.dark' },
]


export const LoginView: m.Component = (() => {
  const state = {
    username: '',
    password: '',
    totpCode: '',
    step: 'credentials' as 'credentials' | 'totp',
    loading: false,
    error: null as string | null,
  }

  if (isAuthEnabled() && getToken()) {
    m.route.set('/')
  }

  async function handleCredentialsSubmit(e: Event): Promise<void> {
    e.preventDefault()
    if (!state.username.trim() || !state.password.trim()) {
      state.error = i18n.t('login.enterCredentials')
      return
    }

    state.loading = true
    state.error = null

    try {
      const res = await fetch(`${API_BASE}/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: state.username, password: state.password }),
      })
      const json = await res.json()

      if (res.ok && json.success) {
        if (json.data?.requireTotp) {
          state.step = 'totp'
        } else if (json.data?.token) {
          setToken(json.data.token)
          m.route.set('/')
        }
      } else if (res.status === 401) {
        state.error = i18n.t('login.invalidCredentials')
      } else {
        state.error = json.message || i18n.t('login.networkError')
      }
    } catch {
      state.error = i18n.t('login.networkError')
    } finally {
      state.loading = false
    }
  }

  async function handleTotpSubmit(e: Event): Promise<void> {
    e.preventDefault()
    if (state.totpCode.length !== 6) {
      state.error = i18n.t('login.enter6DigitCode')
      return
    }

    state.loading = true
    state.error = null

    try {
      const res = await fetch(`${API_BASE}/auth/login/totp`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: state.username, code: state.totpCode }),
      })
      const json = await res.json()

      if (res.ok && json.success && json.data?.token) {
        setToken(json.data.token)
        m.route.set('/')
      } else {
        state.error = json.message || i18n.t('login.invalidCode')
      }
    } catch {
      state.error = i18n.t('login.networkError')
    } finally {
      state.loading = false
    }
  }

  function handleBack(): void {
    state.step = 'credentials'
    state.totpCode = ''
    state.error = null
  }

  return {
    view() {
      const busy = state.loading

      return (
        <div class="md-auth-page">
          <div class="md-auth-card">
            <h1>{i18n.t('login.title')}</h1>

            <div class="md-row md-row--end" style="margin-bottom:16px">
              <div class="md-row md-row--tight">
                <span class="md-muted-sm">{i18n.t('login.language')}</span>
                <select
                  id="login-lang"
                  class="md-select md-select--sm"
                  value={i18n.lang}
                  onchange={(e: Event) => i18n.setLang((e.target as HTMLSelectElement).value as Lang)}
                >
                  {LANG_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </select>
              </div>
              <div class="md-row md-row--tight">
                <span class="md-muted-sm">{i18n.t('login.theme')}</span>
                <select
                  id="login-theme"
                  class="md-select md-select--sm"
                  value={theme.option}
                  onchange={(e: Event) => {
                    theme.setOption((e.target as HTMLSelectElement).value as ThemeOption)
                    m.redraw()
                  }}
                >
                  {THEME_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {i18n.t(opt.labelKey)}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            {state.error && (
              <div class="md-banner md-banner--error" style="margin-bottom:16px">
                {state.error}
              </div>
            )}

            {state.step === 'credentials' ? (
              <form class="md-field-group" onsubmit={(e: Event) => handleCredentialsSubmit(e)}>
                <div>
                  <label class="md-label">{i18n.t('login.username')}</label>
                  <input
                    class="md-field"
                    type="text"
                    value={state.username}
                    placeholder={i18n.t('login.usernamePlaceholder')}
                    disabled={busy}
                    oninput={(e: Event) => {
                      state.username = (e.target as HTMLInputElement).value
                    }}
                  />
                </div>
                <div>
                  <label class="md-label">{i18n.t('login.password')}</label>
                  <input
                    class="md-field"
                    type="password"
                    value={state.password}
                    placeholder={i18n.t('login.passwordPlaceholder')}
                    disabled={busy}
                    oninput={(e: Event) => {
                      state.password = (e.target as HTMLInputElement).value
                    }}
                  />
                </div>
                <button class="md-btn md-btn--filled md-btn--block" type="submit" disabled={busy}>
                  {busy ? i18n.t('login.signingIn') : i18n.t('login.signIn')}
                </button>
              </form>
            ) : (
              <form class="md-field-group" onsubmit={(e: Event) => handleTotpSubmit(e)}>
                <p class="md-muted" style="text-align:center;margin:0">
                  {i18n.t('login.totpTitle')}
                </p>
                <div>
                  <label class="md-label">{i18n.t('login.verificationCode')}</label>
                  <input
                    class="md-field md-centered-code"
                    type="text"
                    maxlength={6}
                    value={state.totpCode}
                    placeholder={i18n.t('login.totpPlaceholder')}
                    disabled={busy}
                    oninput={(e: Event) => {
                      state.totpCode = (e.target as HTMLInputElement).value.replace(/\D/g, '').slice(0, 6)
                    }}
                  />
                </div>
                <button
                  class="md-btn md-btn--filled md-btn--block"
                  type="submit"
                  disabled={busy || state.totpCode.length !== 6}
                >
                  {busy ? i18n.t('login.verifying') : i18n.t('login.verify')}
                </button>
                <button class="md-btn md-btn--block" type="button" onclick={() => handleBack()}>
                  {i18n.t('login.back')}
                </button>
              </form>
            )}
          </div>
        </div>
      )
    },
  }
})()
