import { getToken } from './auth.js'
import { API_BASE } from './config.js'

let connected = false
let lastMessage = null
let interval = '5s'

let source = null
let retryDelay = 1000
let closedByUs = false
let retryTimer

const listeners = new Set()

function emit() {
  listeners.forEach((fn) => fn(lastMessage))
}

function setConnected(next) {
  if (connected === next) return
  connected = next
  emit()
}

function connect() {
  closedByUs = false
  // The server only checks a token when WEB_AUTH is on, so an absent token must
  // not stop us from opening the stream.
  const params = new URLSearchParams({ interval })
  const token = getToken()
  if (token) params.set('token', token)
  const url = `${API_BASE}/events?${params}`

  const es = new EventSource(url)
  source = es
  setConnected(false)

  es.addEventListener('connected', () => {
    retryDelay = 1000
    setConnected(true)
  })

  const parse = (type) => (event) => {
    try {
      const msg = JSON.parse(event.data)
      msg.type = type
      lastMessage = msg
      emit()
    } catch {
      // ignore malformed SSE payload
    }
  }

  es.addEventListener('status_change', parse('status_change'))
  es.addEventListener('uptime_tick', parse('uptime_tick'))

  // The server replaces a connection opened with the same token; this store owns
  // the single app-wide connection, so "replaced" only happens on our teardown.
  es.addEventListener('replaced', () => {
    closedByUs = true
    setConnected(false)
    es.close()
  })

  es.onerror = () => {
    setConnected(false)
    es.close()
    if (closedByUs) return
    const delay = Math.min(retryDelay, 30000)
    retryDelay *= 2
    retryTimer = window.setTimeout(connect, delay)
  }
}

export const sse = {
  get connected() {
    return connected
  },
  get lastMessage() {
    return lastMessage
  },
  get interval() {
    return interval
  },
}

/** Open the connection if it is not already open. Safe to call repeatedly. */
export function startSSE() {
  if (source) return
  connect()
}

/** Close the connection and cancel any pending reconnect. */
export function stopSSE() {
  closedByUs = true
  if (retryTimer !== undefined) {
    window.clearTimeout(retryTimer)
    retryTimer = undefined
  }
  if (source) source.close()
  source = null
  lastMessage = null
  setConnected(false)
}

/** Change the push interval; reconnects with the new value. */
export function setSSEInterval(next) {
  if (next === interval) return
  interval = next
  if (!source) return
  closedByUs = true
  source.close()
  source = null
  retryDelay = 1000
  connect()
}

/** Subscribe to connection state and messages. Returns an unsubscribe function. */
export function onSSE(fn) {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}
