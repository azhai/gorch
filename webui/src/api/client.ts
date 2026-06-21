import { getToken, removeToken } from './authStore'

const API_BASE = '/api'

export interface ServiceStatus {
  name: string
  status: string
  pid: number
  uptime: number
  restartCount: number
  exitCode: number | null
}

export interface LogResponse {
  lines: string[]
  logPath: string
}

export interface ServiceConfig {
  WORK_DIR: string
  EXEC_CMD: string
  RESTART_POLICY: string
  BACK_OFF: number
  STDOUT: string
  STDERR: string
  DEPENDS_ON: string[]
  CRON: string
  ENV_VARS: Record<string, string>
}

export interface APIResponse<T> {
  success: boolean
  message?: string
  data?: T
}

let isRedirecting = false

async function authFetch(url: string, options?: RequestInit): Promise<Response> {
  const token = getToken()
  const headers = new Headers(options?.headers)

  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  const res = await fetch(url, { ...options, headers })

  if (res.status === 401) {
    removeToken()
    if (!isRedirecting) {
      isRedirecting = true
      window.location.href = '/login'
    }
    throw new Error('authentication required')
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

export async function fetchLogs(name: string, lines = 500): Promise<LogResponse> {
  const res = await authFetch(`${API_BASE}/services/${name}/logs?lines=${lines}`)
  const json: APIResponse<LogResponse> = await res.json()
  return json.data || { lines: [], logPath: '' }
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

export async function fetchCronHistory(name: string): Promise<unknown[]> {
  const res = await authFetch(`${API_BASE}/cron/${name}/history`)
  const json: APIResponse<unknown[]> = await res.json()
  return json.data || []
}
