import { getToken } from '../api/authStore'
import { API_BASE } from '../api/config'

export interface SSEMessage {
  type: string
  // Payloads are event-specific ({name,status,…} for status_change and
  // {services:{…}} for uptime_tick); callers narrow by `type`.
  payload: any
  timestamp: number
}

type Listener = (message: SSEMessage | null) => void

let connected = false
let lastMessage: SSEMessage | null = null
let interval = '5s'

let source: EventSource | null = null
let retryDelay = 1000
let closedByUs = false
let retryTimer: number | undefined

const listeners = new Set<Listener>()

function emit(): void {
  listeners.forEach((fn) => fn(lastMessage))
}

function connect(): void {
  const token = getToken()
  if (!token) return

  closedByUs = false
  const url =
    `${API_BASE}/events?token=${encodeURIComponent(token)}` +
    `&interval=${encodeURIComponent(interval)}`

  const es = new EventSource(url)
  source = es
  setConnected(false)

  es.addEventListener('connected', () => {
    retryDelay = 1000
    setConnected(true)
  })

  const parse = (type: string) => (event: MessageEvent) => {
    try {
      const msg = JSON.parse(event.data) as SSEMessage
      msg.type = type
      lastMessage = msg
      emit()
    } catch {
      // ignore malformed SSE payload
    }
  }

  es.addEventListener('status_change', parse('status_change'))
  es.addEventListener('uptime_tick', parse('uptime_tick'))

  // The server replaces a connection opened with the same token (clientID).
  // This store owns the single app-wide connection, so "replaced" only happens
  // on our own teardown — close and stop.
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

function setConnected(next: boolean): void {
  if (connected === next) return
  connected = next
  emit()
}

export const sse = {
  get connected(): boolean {
    return connected
  },
  get lastMessage(): SSEMessage | null {
    return lastMessage
  },
  get interval(): string {
    return interval
  },
}

/** Open the connection if it is not already open. Safe to call repeatedly. */
export function startSSE(): void {
  if (source) return
  connect()
}

/** Close the connection and cancel any pending reconnect. */
export function stopSSE(): void {
  closedByUs = true
  if (retryTimer !== undefined) {
    window.clearTimeout(retryTimer)
    retryTimer = undefined
  }
  source?.close()
  source = null
  lastMessage = null
  setConnected(false)
}

/** Change the push interval; reconnects with the new value. */
export function setSSEInterval(next: string): void {
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
export function onSSE(fn: Listener): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}