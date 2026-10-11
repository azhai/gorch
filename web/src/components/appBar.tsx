import m from 'mithril'
import { ROUTER_BASENAME, STATIC_PREFIX } from '../api/config'
import { getToken, isAuthEnabled, removeToken } from '../api/authStore'
import { authErrorEvent } from '../api/client'
import { i18n } from '../state/i18n'
import { theme, type ThemeOption } from '../state/theme'
import { sse, startSSE, stopSSE } from '../state/sse'
import { dismissToast, getToast } from '../state/toast'

interface NavItem {
  href: string
  labelKey: string
  authOnly?: boolean
}

const NAV_ITEMS: NavItem[] = [
  { href: '/', labelKey: 'nav.dashboard' },
  { href: '/logs', labelKey: 'nav.logs' },
  { href: '/config', labelKey: 'nav.config' },
  { href: '/totp', labelKey: 'nav.security', authOnly: true },
]

const THEME_OPTIONS: { value: ThemeOption; labelKey: string }[] = [
  { value: 'orange', labelKey: 'theme.orange' },
  { value: 'mint', labelKey: 'theme.mint' },
  { value: 'pinkblue', labelKey: 'theme.pinkblue' },
  { value: 'monet', labelKey: 'theme.monet' },
  { value: 'dark', labelKey: 'theme.dark' },
]

const BASE = ROUTER_BASENAME || ''

// There is exactly one app bar, so the auth listener lives at module scope and
// is torn down when the shell is removed.
let detachAuthListener: (() => void) | null = null

function isActive(href: string, path: string): boolean {
  return href === '/' ? path === '/' : path === href || path.startsWith(`${href}/`)
}

function logout(): void {
  removeToken()
  stopSSE()
  m.route.set('/login')
}

/**
 * Persistent top app bar: brand, primary navigation, theme picker, live
 * connection state and logout. Also hosts the app-wide snackbar.
 */
export const AppBar: m.Component = {
  oninit() {
    startSSE()
    const onAuthError = () => logout()
    authErrorEvent.addEventListener('auth-error', onAuthError)
    detachAuthListener = () => authErrorEvent.removeEventListener('auth-error', onAuthError)
  },

  onremove() {
    detachAuthListener?.()
    detachAuthListener = null
  },

  view() {
    const path = m.route.get()
    const showLogout = Boolean(isAuthEnabled() && getToken())
    const toast = getToast()

    return [
      <header class="md-app-bar">
        <div class="md-app-bar__row">
          <div class="md-row" style="gap:24px">
            <div class="md-app-bar__brand">
              <img src={`${STATIC_PREFIX}/logo.svg`} alt="Gorch" />
              <h1>Gorch</h1>
            </div>
            <nav class="md-nav">
              {NAV_ITEMS.filter((item) => !item.authOnly || showLogout).map((item) => (
                <a
                  key={item.href}
                  href={`${BASE}${item.href}`}
                  class={`md-nav__link${isActive(item.href, path) ? ' md-nav__link--active' : ''}`}
                  onclick={(e: Event) => {
                    e.preventDefault()
                    m.route.set(item.href)
                  }}
                >
                  {i18n.t(item.labelKey)}
                </a>
              ))}
            </nav>
          </div>

          <div class="md-app-bar__tools">
            <select
              class="md-select md-select--sm"
              aria-label={i18n.t('login.theme')}
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

            <span class="md-connection">
              <span
                class={`md-connection__dot ${sse.connected ? 'md-connection__dot--on' : 'md-connection__dot--off'}`}
              />
              {sse.connected ? i18n.t('status.connected') : i18n.t('status.reconnecting')}
            </span>

            {showLogout && (
              <button class="md-btn md-btn--sm" onclick={logout}>
                {i18n.t('nav.logout')}
              </button>
            )}
          </div>
        </div>
      </header>,

      toast && (
        <div
          class={`md-snackbar${toast.type === 'error' ? ' md-snackbar--error' : ''}`}
          role="status"
          onclick={dismissToast}
        >
          {toast.message}
        </div>
      ),
    ]
  },
}
