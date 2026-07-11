import {
  createContext,
  useContext,
  useState,
  useEffect,
  useRef,
  useCallback,
  type ReactNode,
} from 'react'
import { getToken } from '../api/authStore'
import { API_BASE } from '../api/config'

interface SSEMessage {
  type: string
  payload: unknown
  timestamp: number
}

interface SSEContextValue {
  connected: boolean
  lastMessage: SSEMessage | null
  interval: string
  setInterval: (interval: string) => void
}

const SSEContext = createContext<SSEContextValue | null>(null)

export function SSEProvider({ children }: { children: ReactNode }) {
  const [connected, setConnected] = useState(false)
  const [lastMessage, setLastMessage] = useState<SSEMessage | null>(null)
  const [interval, setInterval] = useState('5s')

  const esRef = useRef<EventSource | null>(null)
  const retryRef = useRef(1000)
  const intervalRef = useRef(interval)
  const closedRef = useRef(false)

  const connect = useCallback(() => {
    const token = getToken()
    if (!token) return

    closedRef.current = false
    const url = `${API_BASE}/events?token=${encodeURIComponent(token)}&interval=${encodeURIComponent(intervalRef.current)}`
    const es = new EventSource(url)
    esRef.current = es
    setConnected(false)

    es.addEventListener('connected', () => {
      setConnected(true)
      retryRef.current = 1000
    })

    const parseMessage = (type: string) => (event: MessageEvent) => {
      try {
        const msg: SSEMessage = JSON.parse(event.data)
        msg.type = type
        setLastMessage(msg)
      } catch {
        // ignore malformed SSE payload
      }
    }

    es.addEventListener('status_change', parseMessage('status_change'))
    es.addEventListener('uptime_tick', parseMessage('uptime_tick'))

    // The server replaces a connection that opens with the same token
    // (clientID). Since this provider owns the single app-wide connection,
    // a "replaced" event only happens on our own teardown, so we just close.
    es.addEventListener('replaced', () => {
      setConnected(false)
      closedRef.current = true
      es.close()
    })

    es.onerror = () => {
      setConnected(false)
      es.close()
      if (closedRef.current) return
      const delay = Math.min(retryRef.current, 30000)
      retryRef.current *= 2
      setTimeout(connect, delay)
    }
  }, [])

  // Open the connection once on mount.
  useEffect(() => {
    connect()
    return () => {
      closedRef.current = true
      esRef.current?.close()
    }
  }, [connect])

  // Reconnect with the new interval when it changes.
  const changeInterval = useCallback((next: string) => {
    intervalRef.current = next
    setInterval(next)
    if (esRef.current) {
      closedRef.current = true
      esRef.current.close()
      esRef.current = null
      retryRef.current = 1000
    }
    connect()
  }, [connect])

  return (
    <SSEContext.Provider value={{ connected, lastMessage, interval, setInterval: changeInterval }}>
      {children}
    </SSEContext.Provider>
  )
}

export function useSSE() {
  const ctx = useContext(SSEContext)
  if (!ctx) {
    throw new Error('useSSE must be used within an SSEProvider')
  }
  return ctx
}
