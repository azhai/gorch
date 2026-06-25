import { useState, useEffect, useRef, useCallback } from 'react'
import { getToken } from '../api/authStore'
import { API_BASE } from '../api/config'

interface SSEMessage {
  type: string
  payload: unknown
  timestamp: number
}

export function useSSE() {
  const [connected, setConnected] = useState(false)
  const [lastMessage, setLastMessage] = useState<SSEMessage | null>(null)
  const esRef = useRef<EventSource | null>(null)
  const retryRef = useRef(1000)

  const connect = useCallback(() => {
    const token = getToken()
    if (!token) return

    const url = `${API_BASE}/events?token=${encodeURIComponent(token)}`
    const es = new EventSource(url)
    esRef.current = es

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

    es.onerror = () => {
      setConnected(false)
      es.close()
      const delay = Math.min(retryRef.current, 30000)
      retryRef.current *= 2
      setTimeout(connect, delay)
    }
  }, [])

  useEffect(() => {
    connect()
    return () => {
      esRef.current?.close()
    }
  }, [connect])

  return { connected, lastMessage }
}
