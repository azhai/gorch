// Runtime path configuration. window.__URL_PREFIX__ is injected by the backend at
// startup so the same binary works under any sub-path without rebuilding the UI.

const PREFIX = (typeof window !== 'undefined' && window.__URL_PREFIX__) || ''

const clean = PREFIX.replace(/\/+$/, '')

export const API_BASE = clean ? `${clean}/api` : '/api'
export const STATIC_PREFIX = clean
export const ROUTER_BASENAME = clean
