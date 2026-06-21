const TOKEN_KEY = 'gorch_token'

let memoryToken: string | null = null
let useLocalStorage = true

function checkLocalStorage(): boolean {
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

export function getToken(): string | null {
  if (useLocalStorage) {
    return localStorage.getItem(TOKEN_KEY)
  }
  return memoryToken
}

export function setToken(token: string): void {
  if (useLocalStorage) {
    try {
      localStorage.setItem(TOKEN_KEY, token)
      return
    } catch {
      // fallthrough to memory
    }
  }
  memoryToken = token
}

export function removeToken(): void {
  if (useLocalStorage) {
    try {
      localStorage.removeItem(TOKEN_KEY)
    } catch {
      // ignore
    }
  }
  memoryToken = null
}

let authEnabled: boolean | null = null

export function isAuthEnabled(): boolean | null {
  return authEnabled
}

export function setAuthEnabled(enabled: boolean): void {
  authEnabled = enabled
}

export async function detectAuthMode(): Promise<boolean> {
  if (authEnabled !== null) {
    return authEnabled
  }

  try {
    const res = await fetch('/api/services')
    if (res.status === 401) {
      authEnabled = true
    } else {
      authEnabled = false
    }
  } catch {
    authEnabled = false
  }

  return authEnabled
}