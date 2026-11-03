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
  if (currentPath() === path) return
  window.history.pushState(null, '', href(path))
  notify()
}

export function onRouteChange(fn) {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

window.addEventListener('popstate', notify)
