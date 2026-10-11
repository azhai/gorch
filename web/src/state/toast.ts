export type ToastType = 'success' | 'error'

export interface Toast {
  message: string
  type: ToastType
}

const AUTO_HIDE_MS = 3000

let current: Toast | null = null
let hideTimer: number | undefined
const listeners = new Set<() => void>()

function emit(): void {
  listeners.forEach((fn) => fn())
}

/** Show a snackbar; replaces any message already on screen. */
export function showToast(message: string, type: ToastType = 'success'): void {
  current = { message, type }
  if (hideTimer !== undefined) window.clearTimeout(hideTimer)
  hideTimer = window.setTimeout(() => {
    current = null
    hideTimer = undefined
    emit()
  }, AUTO_HIDE_MS)
  emit()
}

export function dismissToast(): void {
  if (hideTimer !== undefined) {
    window.clearTimeout(hideTimer)
    hideTimer = undefined
  }
  if (!current) return
  current = null
  emit()
}

export function getToast(): Toast | null {
  return current
}

export function onToast(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}