import { getToken, removeToken } from './authStore'
import { API_BASE } from './config'

export interface ServiceStatus {
  name: string
  status: string
  pid: number
  memoryMB: number
  exitCode: number | null
  startedAt: number
}

export interface LogResponse {
  lines: string[]
  logPath: string
}

export interface ServiceConfig {
  WORK_DIR: string
  EXEC_CMD: string
  RESTART_CMD: string
  RESTART_POLICY: string
  BACK_OFF: number
  CHECK_PORT: number
  PRE_ACTION: string
  STDOUT: string
  STDERR: string
  DEPENDS_ON: string[]
  CRON: string
  ENV_VARS: Record<string, string>
  PID_FILE: string
}

export interface APIResponse<T> {
  success: boolean
  message?: string
  data?: T
}

export class AuthError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'AuthError'
  }
}

export const authErrorEvent = new EventTarget()

async function authFetch(url: string, options?: RequestInit): Promise<Response> {
  const token = getToken()
  const headers = new Headers(options?.headers)

  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  const res = await fetch(url, { ...options, headers })

  if (res.status === 401) {
    removeToken()
    authErrorEvent.dispatchEvent(new Event('auth-error'))
    throw new AuthError('authentication required')
  }

  return res
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function normalizeServiceData(data: unknown): ServiceStatus[] {
  if (Array.isArray(data)) {
    return data
  }
  if (isObject(data)) {
    return Object.entries(data).map(([key, value]) => {
      const status = value as Record<string, unknown>
      return {
        ...status,
        name: (status.name as string) || key,
      } as ServiceStatus
    })
  }
  if (data !== null && data !== undefined) {
    console.warn('Unexpected services data format:', typeof data)
  }
  return []
}

export async function fetchServices(): Promise<ServiceStatus[]> {
  const res = await authFetch(`${API_BASE}/services`)
  const json: APIResponse<unknown> = await res.json()
  return normalizeServiceData(json.data)
}

export async function fetchService(name: string): Promise<ServiceStatus> {
  const res = await authFetch(`${API_BASE}/services/${name}`)
  const json: APIResponse<ServiceStatus> = await res.json()
  return json.data!
}

export async function startService(name: string): Promise<void> {
  await authFetch(`${API_BASE}/services/${name}/start`, { method: 'POST' })
}

export async function stopService(name: string): Promise<void> {
  await authFetch(`${API_BASE}/services/${name}/stop`, { method: 'POST' })
}

export async function restartService(name: string): Promise<void> {
  await authFetch(`${API_BASE}/services/${name}/restart`, { method: 'POST' })
}

export async function fetchLogs(name: string, lines = 500, type: 'stdout' | 'stderr' = 'stdout'): Promise<LogResponse> {
  const res = await authFetch(`${API_BASE}/services/${name}/logs?lines=${lines}&type=${type}`)
  const json: APIResponse<LogResponse> = await res.json()
  const data = json.data || { lines: [], logPath: '' }
  return { ...data, lines: data.lines || [] }
}

export async function clearLogs(name: string, type: 'stdout' | 'stderr' = 'stdout'): Promise<void> {
  await authFetch(`${API_BASE}/services/${name}/logs/clear?type=${type}`, { method: 'POST' })
}

export async function fetchServiceConfig(name: string): Promise<ServiceConfig> {
  const res = await authFetch(`${API_BASE}/services/${name}/config`)
  const json: APIResponse<ServiceConfig> = await res.json()
  return json.data!
}

export async function updateServiceConfig(name: string, config: ServiceConfig): Promise<APIResponse<{ message: string }>> {
  const res = await authFetch(`${API_BASE}/services/${name}/config`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  })
  return res.json()
}

export async function saveConfigToFile(): Promise<APIResponse<{ message: string }>> {
  const res = await authFetch(`${API_BASE}/save-config`, { method: 'POST' })
  return res.json()
}

export async function createService(name: string, config: ServiceConfig): Promise<APIResponse<{ message: string }>> {
  const res = await authFetch(`${API_BASE}/services`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, svc: config }),
  })
  return res.json()
}

export async function deleteService(name: string): Promise<APIResponse<{ message: string }>> {
  const res = await authFetch(`${API_BASE}/services/${name}`, { method: 'DELETE' })
  return res.json()
}

export interface CronValidationResult {
  valid: boolean
  message: string
  nextRun?: string
  nextRun2?: string
}

export async function validateCronExpression(expression: string): Promise<CronValidationResult> {
  const res = await authFetch(`${API_BASE}/cron/validate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ expression }),
  })
  const json: APIResponse<CronValidationResult> = await res.json()
  return json.data || { valid: false, message: 'no response' }
}

export async function fetchCronHistory(name: string): Promise<unknown[]> {
  const res = await authFetch(`${API_BASE}/cron/${name}/history`)
  const json: APIResponse<unknown[]> = await res.json()
  return json.data || []
}

export interface TOTPSetupResponse {
  secret: string
  qrCode: string
  backupCodes: string[]
}

export interface TOTPStatus {
  enabled: boolean
  backupCodes: number
  hasBinding: boolean
}

export async function totpSetup(): Promise<APIResponse<TOTPSetupResponse>> {
  const res = await authFetch(`${API_BASE}/totp/setup`, { method: 'POST' })
  return res.json()
}

export async function totpVerifySetup(code: string): Promise<APIResponse<void>> {
  const res = await authFetch(`${API_BASE}/totp/verify-setup`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code }),
  })
  return res.json()
}

export async function totpStatus(): Promise<APIResponse<TOTPStatus>> {
  const res = await authFetch(`${API_BASE}/totp/status`)
  return res.json()
}

export async function totpDisable(): Promise<APIResponse<void>> {
  const res = await authFetch(`${API_BASE}/totp/disable`, { method: 'POST' })
  return res.json()
}

export async function totpRegenerateBackupCodes(): Promise<APIResponse<{ backupCodes: string[] }>> {
  const res = await authFetch(`${API_BASE}/totp/backup-codes/regenerate`, { method: 'POST' })
  return res.json()
}
