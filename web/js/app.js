import { API_BASE, STATIC_PREFIX } from './config.js'
import { detectAuthMode, getToken, isAuthEnabled, removeToken } from './auth.js'
import { authErrorEvent, fetchServices } from './api.js'
import { onLangChange, i18n } from './i18n.js'
import { dismissToast, getToast, onToast } from './toast.js'
import { onSSE, setSSEInterval, sse, startSSE, stopSSE } from './sse.js'
import { currentPath, href, navigate, onRouteChange } from './router.js'

const NAV_ITEMS = [
  { path: '/', labelKey: 'nav.dashboard' },
  { path: '/logs', labelKey: 'nav.logs' },
  { path: '/config', labelKey: 'nav.config' },
  { path: '/totp', labelKey: 'nav.security', authOnly: true },
]

/** Root store: routing, auth guard, live service list and the app-wide mirrors of
 *  the i18n / toast / sse singletons so their mutations repaint the templates. */
export function gorch() {
  return {
    route: currentPath(),
    lang: i18n.lang,
    toast: getToast(),
    sseConnected: sse.connected,
    sseInterval: sse.interval,
    services: [],
    now: Math.floor(Date.now() / 1000),
    authEnabled: isAuthEnabled(),
    hasToken: Boolean(getToken()),
    staticPrefix: STATIC_PREFIX,
    navItems: NAV_ITEMS,
    ready: false,
    ticker: undefined,

    async init() {
      document.documentElement.lang = this.lang

      onRouteChange((path) => {
        this.route = path
        this.applyGuard()
      })
      onLangChange(() => {
        this.lang = i18n.lang
        document.documentElement.lang = this.lang
      })
      onToast(() => {
        this.toast = getToast()
      })
      onSSE((message) => {
        this.sseConnected = sse.connected
        if (message) this.applyMessage(message)
      })
      authErrorEvent.addEventListener('auth-error', () => this.logout())

      if (isAuthEnabled() === null) await detectAuthMode()
      this.syncAuth()
      this.applyGuard()

      if (this.route !== '/login') {
        startSSE()
        this.refreshServices()
      }

      this.ready = true

      // One app-wide clock drives every elapsed-uptime label, so views never have
      // to own a timer that leaks when they are swapped out.
      this.ticker = window.setInterval(() => {
        this.now = Math.floor(Date.now() / 1000)
      }, 1000)
    },

    syncAuth() {
      this.authEnabled = isAuthEnabled()
      this.hasToken = Boolean(getToken())
    },

    /** Protected pages require a token once the server announces it wants one. */
    applyGuard() {
      if (this.authEnabled && !this.hasToken && this.route !== '/login') {
        navigate('/login')
        return
      }
      if (this.route === '/login' && (!this.authEnabled || this.hasToken)) {
        navigate('/')
        return
      }
      if (this.route !== '/login') {
        startSSE()
        this.refreshServices()
      }
    },

    get showLogout() {
      return Boolean(this.authEnabled && this.hasToken)
    },

    async refreshServices() {
      try {
        this.services = await fetchServices()
      } catch (err) {
        // A rejected AuthError means the guard is about to redirect to /login.
        if (err instanceof Error && err.name === 'AuthError') return
      }
    },

    applyMessage(message) {
      if (message.type === 'status_change') {
        const p = message.payload
        this.services = this.services.map((svc) =>
          svc.name !== p.name
            ? svc
            : {
                ...svc,
                status: p.status,
                pid: p.pid || 0,
                memoryMB: p.memoryMB,
                startedAt: p.startedAt ?? svc.startedAt,
                exitCode: p.exitCode ?? svc.exitCode,
              }
        )
      }

      if (message.type === 'uptime_tick') {
        const p = message.payload
        this.services = this.services.map((svc) => {
          const info = p.services[svc.name]
          if (info !== undefined) {
            return {
              ...svc,
              pid: info.pid,
              memoryMB: info.memoryMB,
              startedAt: info.startedAt ?? svc.startedAt,
            }
          }
          // The server only reports running services. When one is absent it has no
          // live process - reset startedAt so the elapsed timer stops counting.
          return svc.startedAt === 0 ? svc : { ...svc, startedAt: 0 }
        })
      }
    },

    t(key, vars, fallback) {
      // Reading this.lang keeps every caller's template subscribed to the locale.
      void this.lang
      return i18n.t(key, vars, fallback)
    },

    setLang(next) {
      i18n.setLang(next)
    },

    dismiss() {
      dismissToast()
    },

    go(path) {
      navigate(path)
    },

    hrefOf(path) {
      return href(path)
    },

    isActive(path) {
      if (path === '/') return this.route === '/'
      return this.route === path || this.route.startsWith(`${path}/`)
    },

    asset(name) {
      return `${this.staticPrefix}/${name}`
    },

    changeInterval(next) {
      setSSEInterval(next)
      this.sseInterval = sse.interval
    },

    logout() {
      removeToken()
      stopSSE()
      this.syncAuth()
      this.sseConnected = false
      navigate('/login')
    },
  }
}
