import { API_BASE } from './config.js'

const TOKEN_KEY = 'gorch_token'

let memoryToken = null
let useLocalStorage = true

function checkLocalStorage() {
  try {
    const testKey = '__gorch_test__'
    localStorage.setItem(testKey, '1')
    localStorage.removeItem(testKey)
    return true
  } catch {
    return false
  }
}

useLocalStorage = checkLocalStorage()

export function getToken() {
  if (useLocalStorage) return localStorage.getItem(TOKEN_KEY)
  return memoryToken
}

export function setToken(token) {
  if (useLocalStorage) {
    try {
      localStorage.setItem(TOKEN_KEY, token)
      return
    } catch {
      // fall through to memory
    }
  }
  memoryToken = token
}

export function removeToken() {
  if (useLocalStorage) {
    try {
      localStorage.removeItem(TOKEN_KEY)
    } catch {
      // ignore
    }
  }
  memoryToken = null
}

let authEnabled = null

export function isAuthEnabled() {
  return authEnabled
}

export function setAuthEnabled(enabled) {
  authEnabled = enabled
}

/** Probe an unauthenticated /services to learn whether the server wants a JWT. */
export async function detectAuthMode() {
  if (authEnabled !== null) return authEnabled

  try {
    const res = await fetch(`${API_BASE}/services`)
    authEnabled = res.status === 401
  } catch {
    authEnabled = false
  }

  return authEnabled
}
