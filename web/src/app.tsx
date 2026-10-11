import m from 'mithril'
import { ROUTER_BASENAME } from './api/config'
import { detectAuthMode, getToken, isAuthEnabled } from './api/authStore'
import { AppBar } from './components/appBar'
import { ConfigView } from './views/config'
import { Dashboard } from './views/dashboard'
import { LoginView } from './views/login'
import { LogsView } from './views/logs'
import { TOTPView } from './views/totp'
import { onLangChange } from './state/i18n'
import { onThemeChange } from './state/theme'
import { onToast } from './state/toast'

/** Wraps a page in the app shell and enforces the login redirect. */
function page(view: m.Component) {
  return {
    async oninit() {
      if (isAuthEnabled() === null) await detectAuthMode()
      if (isAuthEnabled() && !getToken()) {
        m.route.set('/login')
      }
    },
    view: () => [m(AppBar), m('main', { class: 'md-content' }, m(view))],
  }
}

export function mountApp(root: Element): void {
  // Mithril defaults to hash routing ("#!"). The Go server serves the SPA for
  // every path (SPA fallback), so switch to history routing and honour the
  // server-injected URL_PREFIX when the UI is mounted on a sub-path.
  m.route.prefix = ROUTER_BASENAME || ''

  // Mithril has no context/provider tree; every store shares one redraw so a
  // language, theme or snackbar change repaints the whole mounted tree.
  const redraw = () => m.redraw()
  onLangChange(redraw)
  onThemeChange(redraw)
  onToast(redraw)

  m.route(
    root,
    '/',
    {
      '/login': LoginView,
      '/': page(Dashboard),
      '/logs': page(LogsView),
      '/config': page(ConfigView),
      '/totp': page(TOTPView),
    }
  )
}
