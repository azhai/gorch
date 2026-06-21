import { useState, useEffect } from 'react'
import { Navigate } from 'react-router-dom'
import { detectAuthMode, isAuthEnabled, getToken } from '../api/authStore'

interface AuthGuardProps {
  children: React.ReactNode
}

export default function AuthGuard({ children }: AuthGuardProps) {
  const [checking, setChecking] = useState(isAuthEnabled() === null)

  useEffect(() => {
    if (isAuthEnabled() === null) {
      detectAuthMode().then(() => setChecking(false))
    }
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

  if (!getToken()) {
    return <Navigate to="/login" replace />
  }

  return <>{children}</>
}