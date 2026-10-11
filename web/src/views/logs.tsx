import m from 'mithril'
import {
  LogResponse,
  ServiceStatus,
  clearLogs,
  fetchLogs,
  fetchServices,
} from '../api/client'
import { i18n } from '../state/i18n'

type LogType = 'stdout' | 'stderr'
const LOG_TYPES: LogType[] = ['stdout', 'stderr']


export const LogsView: m.Component = (() => {
  const state = {
    services: [] as ServiceStatus[],
    selected: '',
    logType: 'stdout' as LogType,
    logData: null as LogResponse | null,
    clearing: false,
    error: null as string | null,
  }

  async function loadServices(): Promise<void> {
    try {
      state.services = await fetchServices()
    } catch {
      // Failure surfaces through the shell auth redirect.
    }
    m.redraw()
  }

  async function loadLogs(): Promise<void> {
    if (!state.selected) {
      state.logData = null
      m.redraw()
      return
    }
    try {
      state.logData = await fetchLogs(state.selected, 500, state.logType)
    } catch (err) {
      state.error = err instanceof Error ? err.message : String(err)
    }
    m.redraw()
  }

  async function handleClear(): Promise<void> {
    if (!state.selected) return
    state.clearing = true
    state.error = null
    try {
      await clearLogs(state.selected, state.logType)
      state.logData = state.logData ? { ...state.logData, lines: [] } : null
    } catch {
      state.error = i18n.t('logs.failedClear')
    } finally {
      state.clearing = false
    }
    m.redraw()
  }

  return {
    oninit() {
      loadServices()
    },

    view() {
      const data = state.logData
      const lines = data?.lines ?? []

      return (
        <div>
          <div class="md-row" style="margin-bottom:16px">
            <h2>{i18n.t('logs.title')}</h2>
            <select
              class="md-select"
              aria-label="Select service to view logs"
              value={state.selected}
              onchange={(e: Event) => {
                state.selected = (e.target as HTMLSelectElement).value
                state.error = null
                loadLogs()
              }}
            >
              <option value="">{i18n.t('logs.selectService')}</option>
              {state.services.map((s) => (
                <option key={s.name} value={s.name}>
                  {s.name}
                </option>
              ))}
            </select>

            {state.selected && (
              <div class="md-segmented">
                {LOG_TYPES.map((type) => (
                  <button
                    key={type}
                    class={`md-segmented__item${state.logType === type ? ' md-segmented__item--selected' : ''}`}
                    onclick={() => {
                      state.logType = type
                      loadLogs()
                    }}
                  >
                    {type === 'stdout' ? i18n.t('logs.stdout') : i18n.t('logs.stderr')}
                  </button>
                ))}
              </div>
            )}

            {state.selected && (
              <button class="md-btn md-btn--danger md-btn--sm" disabled={state.clearing} onclick={() => handleClear()}>
                {state.clearing ? i18n.t('logs.clearing') : i18n.t('logs.clear')}
              </button>
            )}
          </div>

          {state.error && (
            <div class="md-banner md-banner--error" style="margin-bottom:8px">
              {state.error}
            </div>
          )}

          {data?.logPath && (
            <div class="md-muted-sm" style="margin-bottom:8px">
              {i18n.t('logs.source')}: {data.logPath}
            </div>
          )}

          <div class="md-log-pane">
            {lines.length === 0 ? (
              <p class="md-log-empty">{i18n.t('logs.noLogs')}</p>
            ) : (
              lines.map((line, idx) => (
                <div key={idx} class="md-log-line">
                  {line}
                </div>
              ))
            )}
          </div>
        </div>
      )
    },
  }
})()
