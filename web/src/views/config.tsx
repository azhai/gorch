import m from 'mithril'
import {
  ServiceConfig,
  ServiceStatus,
  createService,
  deleteService,
  fetchServiceConfig,
  fetchServices,
  saveConfigToFile,
  updateServiceConfig,
  validateCronExpression,
} from '../api/client'
import { SelectField, TextField } from '../components/fields'
import { i18n } from '../state/i18n'

type MsgType = 'info' | 'success' | 'error'

interface CronResult {
  valid: boolean
  message: string
  nextRun?: string
  nextRun2?: string
}

const RESTART_POLICIES = [
  { value: 'always', label: 'always' },
  { value: 'on-failure', label: 'on-failure' },
  { value: 'never', label: 'never' },
]

const BANNER_CLASS: Record<MsgType, string> = {
  error: 'md-banner--error',
  success: 'md-banner--success',
  info: 'md-banner--info',
}

function blankConfig(): ServiceConfig {
  return {
    WORK_DIR: '.',
    EXEC_CMD: '',
    RESTART_CMD: '',
    RESTART_POLICY: 'never',
    BACK_OFF: 0,
    CHECK_PORT: 0,
    PRE_ACTION: '',
    STDOUT: '',
    STDERR: '',
    DEPENDS_ON: [],
    CRON: '',
    ENV_VARS: {},
    PID_FILE: '',
  }
}

export const ConfigView: m.Component = (() => {
  const state = {
    services: [] as ServiceStatus[],
    selected: '',
    config: null as ServiceConfig | null,
    originalConfig: null as ServiceConfig | null,
    saving: false,
    message: null as string | null,
    messageType: 'info' as MsgType,
    isCreating: false,
    creatingName: '',
    cronValidating: false,
    cronResult: null as CronResult | null,
  }

  function showMsg(msg: string, type: MsgType = 'info'): void {
    state.message = msg
    state.messageType = type
  }

  function hasChanges(): boolean {
    const { config, originalConfig } = state
    return Boolean(config && originalConfig && JSON.stringify(config) !== JSON.stringify(originalConfig))
  }

  function updateField<K extends keyof ServiceConfig>(key: K, value: ServiceConfig[K]): void {
    if (!state.config) return
    state.config = { ...state.config, [key]: value }
    if (key === 'CRON') state.cronResult = null
  }

  function renameEnvVar(oldKey: string, newKey: string, value: string): void {
    if (!state.config) return
    if (newKey !== oldKey && state.config.ENV_VARS[newKey] !== undefined) return
    const envs = { ...state.config.ENV_VARS }
    delete envs[oldKey]
    envs[newKey] = value
    updateField('ENV_VARS', envs)
  }

  function addEnvVar(): void {
    if (!state.config) return
    const base = 'NEW_VAR'
    let name = base
    let i = 1
    while (state.config.ENV_VARS[name] !== undefined) {
      name = `${base}_${i++}`
    }
    updateField('ENV_VARS', { ...state.config.ENV_VARS, [name]: '' })
  }

  function removeEnvVar(key: string): void {
    if (!state.config) return
    const envs = { ...state.config.ENV_VARS }
    delete envs[key]
    updateField('ENV_VARS', envs)
  }

  async function loadServices(): Promise<void> {
    try {
      state.services = await fetchServices()
    } catch {
      // Failure surfaces through the shell auth redirect.
    }
    m.redraw()
  }

  async function selectService(name: string): Promise<void> {
    state.selected = name
    state.cronResult = null
    if (!name) {
      state.config = null
      state.originalConfig = null
      m.redraw()
      return
    }
    const cfg = await fetchServiceConfig(name)
    cfg.DEPENDS_ON = cfg.DEPENDS_ON || []
    cfg.ENV_VARS = cfg.ENV_VARS || []
    state.config = cfg
    state.originalConfig = JSON.parse(JSON.stringify(cfg))
    m.redraw()
  }

  async function handleSaveAndApply(): Promise<void> {
    const { config, selected } = state
    if (!config || !selected) return
    state.saving = true
    state.message = null

    // Step 1: apply in-memory changes (this also refreshes the cron scheduler).
    if (hasChanges()) {
      const applyRes = await updateServiceConfig(selected, config)
      if (!applyRes.success) {
        showMsg(applyRes.message || i18n.t('config.updateFailed'), 'error')
        state.saving = false
        m.redraw()
        return
      }
      state.originalConfig = JSON.parse(JSON.stringify(config))
    }

    // Step 2: persist to the config file.
    const saveRes = await saveConfigToFile()
    if (!saveRes.success) {
      showMsg(saveRes.message || i18n.t('config.saveToFileFailed'), 'error')
    } else {
      showMsg(i18n.t('config.savedAndApplied'), 'success')
    }

    state.saving = false
    m.redraw()
  }

  async function handleCreate(): Promise<void> {
    const name = state.creatingName.trim()
    if (!name) {
      showMsg(i18n.t('config.serviceNameRequired'), 'error')
      return
    }
    if (!state.config?.EXEC_CMD.trim()) {
      showMsg(i18n.t('config.execCmdRequired'), 'error')
      return
    }
    state.saving = true
    state.message = null

    const res = await createService(name, state.config)
    if (res.success) {
      state.isCreating = false
      state.creatingName = ''
      state.selected = ''
      state.config = null
      state.originalConfig = null
      showMsg(i18n.t('config.serviceCreated'), 'success')
      loadServices()
    } else {
      showMsg(res.message || i18n.t('config.createFailed'), 'error')
    }

    state.saving = false
    m.redraw()
  }

  async function handleDelete(): Promise<void> {
    const name = state.selected
    if (!name) return
    if (!confirm(i18n.t('config.deleteConfirm', { name }))) return
    state.saving = true
    state.message = null

    const res = await deleteService(name)
    if (res.success) {
      state.selected = ''
      state.config = null
      state.originalConfig = null
      showMsg(i18n.t('config.serviceDeleted'), 'success')
      loadServices()
    } else {
      showMsg(res.message || i18n.t('config.deleteFailed'), 'error')
    }

    state.saving = false
    m.redraw()
  }

  async function handleValidateCron(): Promise<void> {
    const expr = state.config?.CRON
    if (!expr) return
    state.cronValidating = true
    state.cronResult = null
    try {
      state.cronResult = await validateCronExpression(expr)
    } catch (err) {
      state.cronResult = {
        valid: false,
        message: err instanceof Error ? err.message : i18n.t('config.networkError'),
      }
    } finally {
      state.cronValidating = false
    }
    m.redraw()
  }

  function startCreate(): void {
    state.selected = ''
    state.creatingName = ''
    state.config = blankConfig()
    state.originalConfig = null
    state.isCreating = true
    state.message = null
  }

  function cancelCreate(): void {
    state.isCreating = false
    state.creatingName = ''
    state.selected = ''
    state.config = null
    state.originalConfig = null
    state.message = null
  }

  return {
    oninit() {
      loadServices()
    },

    view() {
      const cfg = state.config
      const envEntries = cfg ? Object.entries(cfg.ENV_VARS) : []
      const busy = state.saving

      return (
        <div>
          <div class="md-row" style="margin-bottom:16px">
            <h2>{i18n.t('config.title')}</h2>
            {m(SelectField, {
              class: 'md-select',
              ariaLabel: 'Select service to configure',
              value: state.selected,
              options: [
                { value: '', label: i18n.t('config.selectService') },
                ...state.services.map((s) => ({ value: s.name, label: s.name })),
              ],
              onchange: (value: string) => {
                state.message = null
                state.isCreating = false
                selectService(value)
              },
            })}
            <button
              class="md-btn md-btn--tonal md-btn--sm"
              aria-label="Create new service"
              onclick={() => startCreate()}
            >
              {i18n.t('config.new')}
            </button>
            {state.selected && !state.isCreating && (
              <button
                class="md-btn md-btn--danger md-btn--sm"
                aria-label={`Delete service ${state.selected}`}
                onclick={() => handleDelete()}
              >
                {i18n.t('config.delete')}
              </button>
            )}
          </div>

          {!cfg ? (
            <div class="md-card">
              <p class="md-muted">{i18n.t('config.selectHint')}</p>
            </div>
          ) : (
            <div class="md-card md-field-group">
              {state.isCreating &&
                m(TextField, {
                  label: `${i18n.t('config.serviceName')} *`,
                  value: state.creatingName,
                  placeholder: i18n.t('config.serviceNamePlaceholder'),
                  oninput: (value: string) => {
                    state.creatingName = value
                  },
                })}

              {m(TextField, {
                label: 'EXEC_CMD',
                hint: i18n.t('config.command'),
                value: cfg.EXEC_CMD,
                oninput: (value: string) => updateField('EXEC_CMD', value),
              })}

              {m(TextField, {
                label: 'RESTART_CMD',
                hint: i18n.t('config.gracefulReload'),
                value: cfg.RESTART_CMD || '',
                placeholder: 'e.g. nginx -s reload',
                oninput: (value: string) => updateField('RESTART_CMD', value),
              })}

              {m(TextField, {
                label: 'PID_FILE',
                hint: i18n.t('config.pidFile'),
                value: cfg.PID_FILE || '',
                placeholder: 'e.g. /var/run/nginx.pid',
                oninput: (value: string) => updateField('PID_FILE', value),
              })}

              {m(TextField, {
                label: 'PRE_ACTION',
                hint: i18n.t('config.preAction'),
                value: cfg.PRE_ACTION || '',
                placeholder: 'e.g. pkill -f myservice',
                oninput: (value: string) => updateField('PRE_ACTION', value),
              })}

              {m(TextField, {
                label: 'WORK_DIR',
                hint: i18n.t('config.workingDir'),
                value: cfg.WORK_DIR,
                oninput: (value: string) => updateField('WORK_DIR', value),
              })}

              <div class="md-grid-3">
                {m(SelectField, {
                  label: 'RESTART_POLICY',
                  hint: i18n.t('config.restartPolicy'),
                  value: cfg.RESTART_POLICY,
                  options: RESTART_POLICIES,
                  onchange: (value: string) => updateField('RESTART_POLICY', value),
                })}
                {m(TextField, {
                  label: 'CHECK_PORT',
                  hint: `0 = ${i18n.t('config.off')}`,
                  type: 'number',
                  value: String(cfg.CHECK_PORT || 0),
                  placeholder: '0',
                  oninput: (value: string) => updateField('CHECK_PORT', parseInt(value, 10) || 0),
                })}
                {m(TextField, {
                  label: 'BACK_OFF',
                  hint: i18n.t('config.backOff'),
                  type: 'number',
                  value: String(cfg.BACK_OFF),
                  oninput: (value: string) => updateField('BACK_OFF', parseInt(value, 10) || 0),
                })}
              </div>

              <div class="md-grid-2">
                {m(TextField, {
                  label: 'STDOUT',
                  hint: i18n.t('config.logOutput'),
                  value: cfg.STDOUT,
                  oninput: (value: string) => updateField('STDOUT', value),
                })}
                {m(TextField, {
                  label: 'STDERR',
                  hint: i18n.t('config.errorOutput'),
                  value: cfg.STDERR,
                  oninput: (value: string) => updateField('STDERR', value),
                })}
              </div>

              <div>
                {m(TextField, {
                  label: 'CRON',
                  hint: i18n.t('config.schedule'),
                  value: cfg.CRON || '',
                  placeholder: i18n.t('config.cronPlaceholder'),
                  oninput: (value: string) => updateField('CRON', value),
                })}
                <div class="md-row" style="margin-top:8px">
                  <button
                    class="md-btn md-btn--tonal md-btn--sm"
                    disabled={!cfg.CRON || state.cronValidating}
                    onclick={() => handleValidateCron()}
                  >
                    {state.cronValidating ? i18n.t('config.checking') : i18n.t('config.validate')}
                  </button>
                </div>
                {state.cronResult && (
                  <div
                    class={`md-banner md-banner--compact ${state.cronResult.valid ? 'md-banner--success' : 'md-banner--error'}`}
                    style="margin-top:8px"
                  >
                    {state.cronResult.valid
                      ? i18n.t('config.cronValid', {
                          next1: state.cronResult.nextRun || '',
                          next2: state.cronResult.nextRun2 || '',
                        })
                      : i18n.t('config.cronInvalid', { message: state.cronResult.message })}
                  </div>
                )}
              </div>

              {m(TextField, {
                label: 'DEPENDS_ON',
                hint: i18n.t('config.dependencies'),
                value: cfg.DEPENDS_ON.join(', '),
                placeholder: i18n.t('config.depsPlaceholder'),
                oninput: (value: string) =>
                  updateField(
                    'DEPENDS_ON',
                    value.split(',').map((s) => s.trim()).filter(Boolean)
                  ),
              })}

              <div>
                <div class="md-row md-row--between" style="margin-bottom:4px">
                  <span class="md-label" style="margin:0">
                    ENV_VARS <span class="md-label__hint">({i18n.t('config.environment')})</span>
                  </span>
                  <button class="md-btn md-btn--sm" onclick={() => addEnvVar()}>
                    {i18n.t('config.add')}
                  </button>
                </div>
                <div class="md-field-group">
                  {envEntries.map(([key, value]) => (
                    <div key={key} class="md-row md-row--tight">
                      <input
                        class="md-field md-field--sm"
                        style="width:33%"
                        value={key}
                        placeholder={i18n.t('config.key')}
                        oninput={(e: Event) => renameEnvVar(key, (e.target as HTMLInputElement).value, value)}
                      />
                      <input
                        class="md-field md-field--sm"
                        style="flex:1"
                        value={value}
                        placeholder={i18n.t('config.value')}
                        oninput={(e: Event) => renameEnvVar(key, key, (e.target as HTMLInputElement).value)}
                      />
                      <button
                        class="md-btn md-btn--danger md-btn--sm"
                        aria-label={`Remove environment variable ${key}`}
                        onclick={() => removeEnvVar(key)}
                      >
                        x
                      </button>
                    </div>
                  ))}
                  {envEntries.length === 0 && <p class="md-muted-sm">{i18n.t('config.noEnvVars')}</p>}
                </div>
              </div>

              {state.message && (
                <div class={`md-banner ${BANNER_CLASS[state.messageType]}`}>{state.message}</div>
              )}

              <div class="md-row md-row--tight">
                {state.isCreating ? (
                  <button
                    class="md-btn md-btn--filled"
                    disabled={!state.creatingName.trim() || !cfg.EXEC_CMD.trim() || busy}
                    onclick={() => handleCreate()}
                  >
                    {state.saving ? i18n.t('config.creating') : i18n.t('config.createService')}
                  </button>
                ) : (
                  <button class="md-btn md-btn--filled" disabled={busy} onclick={() => handleSaveAndApply()}>
                    {state.saving ? i18n.t('config.saving') : i18n.t('config.saveAndApply')}
                  </button>
                )}
                {state.isCreating && (
                  <button class="md-btn" onclick={() => cancelCreate()}>
                    {i18n.t('config.cancel')}
                  </button>
                )}
              </div>
            </div>
          )}
        </div>
      )
    },
  }
})()
