const AUTO_HIDE_MS = 3000

let current = null
let hideTimer

const listeners = new Set()

function emit() {
  listeners.forEach((fn) => fn())
}

/** Show a snackbar; replaces any message already on screen. */
export function showToast(message, type = 'success') {
  current = { message, type }
  if (hideTimer !== undefined) window.clearTimeout(hideTimer)
  hideTimer = window.setTimeout(() => {
    current = null
    hideTimer = undefined
    emit()
  }, AUTO_HIDE_MS)
  emit()
}

export function dismissToast() {
  if (hideTimer !== undefined) {
    window.clearTimeout(hideTimer)
    hideTimer = undefined
  }
  if (!current) return
  current = null
  emit()
}

export function getToast() {
  return current
}

export function onToast(fn) {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}
