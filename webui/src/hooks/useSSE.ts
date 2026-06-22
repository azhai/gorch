import { useState, useEffect, useRef, useCallback } from 'react'
import { getToken } from '../api/authStore'

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

    const url = `/api/events?token=${encodeURIComponent(token)}`
    const es = new EventSource(url)
    esRef.current = es

    es.addEventListener('connected', () => {
      setConnected(true)
      retryRef.current = 1000
    })

    es.addEventListener('status_change', (event) => {
      try {
        const msg: SSEMessage = JSON.parse(event.data)
        msg.type = 'status_change'
        setLastMessage(msg)
      } catch {
        // ignore
      }
    })

    es.addEventListener('uptime_tick', (event) => {
      try {
        const msg: SSEMessage = JSON.parse(event.data)
        msg.type = 'uptime_tick'
        setLastMessage(msg)
      } catch {
        // ignore
      }
    })

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
