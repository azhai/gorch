// Runtime path configuration. window.__URL_PREFIX__ is injected by the
// Go backend at startup (see internal/web/server.go initIndexHTML) so the
// same binary works under any sub-path without rebuilding the frontend.

const PREFIX =
  (typeof window !== 'undefined' && (window as { __URL_PREFIX__?: string }).__URL_PREFIX__) ||
  ''

const clean = PREFIX.replace(/\/+$/, '')

export const API_BASE = clean ? `${clean}/api` : '/api'
export const STATIC_PREFIX = clean
export const ROUTER_BASENAME = clean || undefined
