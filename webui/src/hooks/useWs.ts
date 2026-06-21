import { useState, useEffect, useRef, useCallback } from 'react'

interface WsMessage {
  type: string
  payload: unknown
  timestamp: number
}

export function useWs() {
  const [connected, setConnected] = useState(false)
  const [lastMessage, setLastMessage] = useState<WsMessage | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const retryRef = useRef(1000)

  const connect = useCallback(() => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/api/ws`
    const ws = new WebSocket(wsUrl)
    wsRef.current = ws

    ws.onopen = () => {
      setConnected(true)
      retryRef.current = 1000
    }

    ws.onmessage = (event) => {
      try {
        const msg: WsMessage = JSON.parse(event.data)
        setLastMessage(msg)
      } catch {
        // ignore
      }
    }

    ws.onclose = () => {
      setConnected(false)
      const delay = Math.min(retryRef.current, 30000)
      retryRef.current *= 2
      setTimeout(connect, delay)
    }

    ws.onerror = () => {
      ws.close()
    }
  }, [])

  useEffect(() => {
    connect()
    return () => {
      wsRef.current?.close()
    }
  }, [connect])

  return { connected, lastMessage }
}