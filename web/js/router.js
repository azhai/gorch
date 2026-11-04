import { ROUTER_BASENAME } from './config.js'

// The backend serves the SPA for every path, so routing uses history and honours
// the server-injected URL_PREFIX when the UI is mounted on a sub-path.
const BASE = ROUTER_BASENAME || ''

const listeners = new Set()

function notify() {
  listeners.forEach((fn) => fn(currentPath()))
}

export function currentPath() {
  let path = window.location.pathname
  if (BASE && path.startsWith(BASE)) path = path.slice(BASE.length)
  if (!path.startsWith('/')) path = `/${path}`
  const trimmed = path.replace(/\/+$/, '')
  return trimmed || '/'
}

export function href(path) {
  return `${BASE}${path}`
}

export function navigate(path) {
  // Compare the whole target, not just the pathname: a card links into
  // /config?service=x, and the query is what carries the selection.
  if (href(path) === `${window.location.pathname}${window.location.search}`) return
  window.history.pushState(null, '', href(path))
  notify()
}

/** A query parameter of the current URL, or '' when absent. */
export function queryParam(key) {
  return new URLSearchParams(window.location.search).get(key) || ''
}

/**
 * Applies `?service=<name>` to a view's picker, which is how the dashboard
 * cards link into the config and log pages.
 *
 * A hard load (a bookmark, a refresh) renders the view before the service list
 * has arrived, and a <select> cannot hold a value for an option that is not
 * there yet - it would silently fall back to the placeholder. So the selection
 * is deferred until the list actually holds the name.
 */
export function selectFromQuery(scope, select) {
  const wanted = queryParam('service')
  if (!wanted) return

  const known = () => (scope.$data.services || []).some((s) => s.name === wanted)
  if (known()) {
    // init() runs before the view's directives do, so the <select> has no
    // options to hold a value yet; assigning it now would be dropped and the
    // picker would keep showing the placeholder. Let Alpine render first.
    scope.$nextTick(() => select(wanted))
    return
  }

  let done = false
  scope.$watch(
    () => (scope.$data.services || []).map((s) => s.name).join('\u0000'),
    () => {
      // A name that never shows up (deleted, or a stale link) stays unselected;
      // the view then shows its own "pick a service" state.
      if (done || !known()) return
      done = true
      select(wanted)
    }
  )
}

export function onRouteChange(fn) {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

window.addEventListener('popstate', notify)
