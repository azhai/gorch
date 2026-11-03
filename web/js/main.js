// Alpine.js 3.17.5 is vendored rather than pulled from a CDN: the SPA is embedded in
// the binary and served on a LAN/router, where a CDN would blank the UI offline.
// The ESM build is used so components are registered before Alpine.start().
import Alpine from '../vendor/alpine.esm.min.js'
import { gorch } from './app.js'
import { dashboardView } from './views/dashboard.js'
import { logsView } from './views/logs.js'
import { configView } from './views/config.js'
import { loginView } from './views/login.js'
import { totpView } from './views/totp.js'

// Monet is the only remaining palette, so the token block is fixed instead of selectable.
document.documentElement.dataset.theme = 'monet'

Alpine.data('gorch', gorch)
Alpine.data('dashboardView', dashboardView)
Alpine.data('logsView', logsView)
Alpine.data('configView', configView)
Alpine.data('loginView', loginView)
Alpine.data('totpView', totpView)

Alpine.start()
