import { useState, useEffect } from 'react'
import { Navigate } from 'react-router-dom'
import { detectAuthMode, isAuthEnabled, getToken } from '../api/authStore'
import { authErrorEvent } from '../api/client'

interface AuthGuardProps {
  children: React.ReactNode
}

export default function AuthGuard({ children }: AuthGuardProps) {
  const [checking, setChecking] = useState(isAuthEnabled() === null)
  const [authLost, setAuthLost] = useState(false)

  useEffect(() => {
    if (isAuthEnabled() === null) {
      detectAuthMode().then(() => setChecking(false))
    }

    const onAuthError = () => setAuthLost(true)
    authErrorEvent.addEventListener('auth-error', onAuthError)
    return () => authErrorEvent.removeEventListener('auth-error', onAuthError)
  }, [])

  if (checking) {
    return (
      <div className="min-h-screen bg-macaron-cream flex items-center justify-center">
        <div className="text-gray-400 text-sm">Loading...</div>
      </div>
    )
  }

  if (!isAuthEnabled()) {
    return <>{children}</>
  }

  if (!getToken() || authLost) {
    return <Navigate to="/login" replace />
  }

  return <>{children}</>
}