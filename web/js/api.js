import { API_BASE } from './config.js'
import { getToken, removeToken } from './auth.js'

export class AuthError extends Error {
  constructor(message) {
    super(message)
    this.name = 'AuthError'
  }
}

export const authErrorEvent = new EventTarget()

async function authFetch(url, options) {
  const token = getToken()
  const headers = new Headers(options && options.headers)

  if (token) headers.set('Authorization', `Bearer ${token}`)

  const res = await fetch(url, { ...options, headers })

  if (res.status === 401) {
    removeToken()
    authErrorEvent.dispatchEvent(new Event('auth-error'))
    throw new AuthError('authentication required')
  }

  return res
}

function isObject(value) {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/** The server answers /services with either a list or a name-keyed map. */
function normalizeServiceData(data) {
  if (Array.isArray(data)) return data

  if (isObject(data)) {
    return Object.entries(data).map(([key, value]) => {
      const status = value || {}
      return { ...status, name: status.name || key }
    })
  }

  if (data !== null && data !== undefined) {
    console.warn('Unexpected services data format:', typeof data)
  }
  return []
}

export async function fetchServices() {
  const res = await authFetch(`${API_BASE}/services`)
  const json = await res.json()
  return normalizeServiceData(json.data)
}

export async function fetchService(name) {
  const res = await authFetch(`${API_BASE}/services/${name}`)
  const json = await res.json()
  return json.data
}

export async function startService(name) {
  await authFetch(`${API_BASE}/services/${name}/start`, { method: 'POST' })
}

export async function stopService(name) {
  await authFetch(`${API_BASE}/services/${name}/stop`, { method: 'POST' })
}

export async function restartService(name) {
  await authFetch(`${API_BASE}/services/${name}/restart`, { method: 'POST' })
}

export async function fetchLogs(name, lines = 500, type = 'stdout') {
  const res = await authFetch(`${API_BASE}/services/${name}/logs?lines=${lines}&type=${type}`)
  const json = await res.json()
  const data = json.data || { lines: [], logPath: '' }
  return { ...data, lines: data.lines || [] }
}

export async function clearLogs(name, type = 'stdout') {
  await authFetch(`${API_BASE}/services/${name}/logs/clear?type=${type}`, { method: 'POST' })
}

export async function fetchServiceConfig(name) {
  const res = await authFetch(`${API_BASE}/services/${name}/config`)
  const json = await res.json()
  return json.data
}

export async function updateServiceConfig(name, config) {
  const res = await authFetch(`${API_BASE}/services/${name}/config`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  })
  return res.json()
}

export async function saveConfigToFile() {
  const res = await authFetch(`${API_BASE}/save-config`, { method: 'POST' })
  return res.json()
}

export async function createService(name, config) {
  const res = await authFetch(`${API_BASE}/services`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, svc: config }),
  })
  return res.json()
}

export async function deleteService(name) {
  const res = await authFetch(`${API_BASE}/services/${name}`, { method: 'DELETE' })
  return res.json()
}

export async function validateCronExpression(expression) {
  const res = await authFetch(`${API_BASE}/cron/validate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ expression }),
  })
  const json = await res.json()
  return json.data || { valid: false, message: 'no response' }
}

export async function fetchCronHistory(name) {
  const res = await authFetch(`${API_BASE}/cron/${name}/history`)
  const json = await res.json()
  return json.data || []
}

export async function totpSetup() {
  const res = await authFetch(`${API_BASE}/totp/setup`, { method: 'POST' })
  return res.json()
}

export async function totpVerifySetup(code) {
  const res = await authFetch(`${API_BASE}/totp/verify-setup`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code }),
  })
  return res.json()
}

export async function totpStatus() {
  const res = await authFetch(`${API_BASE}/totp/status`)
  return res.json()
}

export async function totpDisable() {
  const res = await authFetch(`${API_BASE}/totp/disable`, { method: 'POST' })
  return res.json()
}

export async function totpRegenerateBackupCodes() {
  const res = await authFetch(`${API_BASE}/totp/backup-codes/regenerate`, { method: 'POST' })
  return res.json()
}

export async function login(username, password) {
  const res = await fetch(`${API_BASE}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  return { ok: res.ok, status: res.status, json: await res.json().catch(() => ({})) }
}

export async function loginTotp(username, code) {
  const res = await fetch(`${API_BASE}/auth/login/totp`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, code }),
  })
  return { ok: res.ok, status: res.status, json: await res.json().catch(() => ({})) }
}
