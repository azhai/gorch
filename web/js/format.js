export const STATUS_CLASS = {
  running: 'md-status-dot--running',
  stopped: 'md-status-dot--stopped',
  failed: 'md-status-dot--failed',
  crashed: 'md-status-dot--crashed',
  starting: 'md-status-dot--starting',
}

/** `now` is passed in (not read from Date) so Alpine tracks it as a dependency. */
export function formatUptimeElapsed(startedAt, now) {
  if (!startedAt) return '-'
  const seconds = Math.floor(now - startedAt)
  if (seconds <= 0) return '-'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m ${seconds % 60}s`
}

export function formatStartTime(startedAt) {
  if (!startedAt) return '-'
  const d = new Date(startedAt * 1000)
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  const hh = String(d.getHours()).padStart(2, '0')
  const min = String(d.getMinutes()).padStart(2, '0')
  return `${mm}-${dd} ${hh}:${min}`
}

export function formatUptime(startedAt, mode, now) {
  return mode === 'start' ? formatStartTime(startedAt) : formatUptimeElapsed(startedAt, now)
}

/** A finished run's duration, kept short enough for a single table row. */
export function formatDuration(seconds) {
  if (!Number.isFinite(seconds) || seconds < 0) return '-'
  if (seconds < 60) return `${seconds}s`
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  return s ? `${m}m ${s}s` : `${m}m`
}
