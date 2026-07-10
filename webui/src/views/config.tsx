import { useState, useEffect } from 'react'
import { fetchServices, fetchServiceConfig, updateServiceConfig, saveConfigToFile, createService, deleteService, validateCronExpression, ServiceStatus, ServiceConfig } from '../api/client'
import { useI18n } from '../i18n/I18nProvider'

const inputClass = 'w-full px-3 py-1.5 text-sm border border-macaron-peach dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50'

export default function Config() {
  const [services, setServices] = useState<ServiceStatus[]>([])
  const [selected, setSelected] = useState<string>('')
  const [config, setConfig] = useState<ServiceConfig | null>(null)
  const [originalConfig, setOriginalConfig] = useState<ServiceConfig | null>(null)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [messageType, setMessageType] = useState<'info' | 'success' | 'error'>('info')
  const [isCreating, setIsCreating] = useState(false)
  const [creatingName, setCreatingName] = useState('')
  const [cronValidating, setCronValidating] = useState(false)
  const [cronResult, setCronResult] = useState<{ valid: boolean; message: string; nextRun?: string; nextRun2?: string } | null>(null)
  const { t } = useI18n()

  useEffect(() => {
    fetchServices().then(setServices)
  }, [])

  useEffect(() => {
    if (!selected) {
      setConfig(null)
      setOriginalConfig(null)
      setCronResult(null)
      return
    }
    fetchServiceConfig(selected).then((cfg) => {
      cfg.DEPENDS_ON = cfg.DEPENDS_ON || []
      cfg.ENV_VARS = cfg.ENV_VARS || {}
      setConfig(cfg)
      setOriginalConfig(JSON.parse(JSON.stringify(cfg)))
      setCronResult(null)
    })
  }, [selected])

  const hasChanges = config && originalConfig && JSON.stringify(config) !== JSON.stringify(originalConfig)

  const showMsg = (msg: string, type: 'info' | 'success' | 'error' = 'info') => {
    setMessage(msg)
    setMessageType(type)
  }

  const handleSave = async () => {
    if (!config || !selected) return
    setSaving(true)
    setMessage(null)
    try {
      const res = await updateServiceConfig(selected, config)
      if (res.success) {
        setOriginalConfig(JSON.parse(JSON.stringify(config)))
        showMsg(t('config.applied'), 'success')
      } else {
        showMsg(res.message || t('config.updateFailed'), 'error')
      }
    } catch (err) {
      showMsg(err instanceof Error ? err.message : t('config.networkError'), 'error')
    } finally {
      setSaving(false)
    }
  }

  const handleSaveToFile = async () => {
    setSaving(true)
    setMessage(null)
    try {
      const res = await saveConfigToFile()
      if (res.success) {
        showMsg(t('config.savedToFile'), 'success')
      } else {
        showMsg(res.message || t('config.saveToFileFailed'), 'error')
      }
    } catch (err) {
      showMsg(err instanceof Error ? err.message : t('config.networkError'), 'error')
    } finally {
      setSaving(false)
    }
  }

  const handleCreate = async () => {
    const name = creatingName.trim()
    if (!name) {
      showMsg(t('config.serviceNameRequired'), 'error')
      return
    }
    if (!config || !config.EXEC_CMD.trim()) {
      showMsg(t('config.execCmdRequired'), 'error')
      return
    }
    setSaving(true)
    setMessage(null)
    try {
      const res = await createService(name, config)
      if (res.success) {
        setIsCreating(false)
        setCreatingName('')
        setSelected(name)
        fetchServices().then(setServices)
        showMsg(t('config.serviceCreated'), 'success')
      } else {
        showMsg(res.message || t('config.createFailed'), 'error')
      }
    } catch (err) {
      showMsg(err instanceof Error ? err.message : t('config.networkError'), 'error')
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!selected) return
    if (!confirm(t('config.deleteConfirm', { name: selected }))) return
    setSaving(true)
    setMessage(null)
    try {
      const res = await deleteService(selected)
      if (res.success) {
        setSelected('')
        setConfig(null)
        setOriginalConfig(null)
        fetchServices().then(setServices)
        showMsg(t('config.serviceDeleted'), 'success')
      } else {
        showMsg(res.message || t('config.deleteFailed'), 'error')
      }
    } catch (err) {
      showMsg(err instanceof Error ? err.message : t('config.networkError'), 'error')
    } finally {
      setSaving(false)
    }
  }

  const handleValidateCron = async () => {
    if (!config?.CRON) return
    setCronValidating(true)
    setCronResult(null)
    try {
      const res = await validateCronExpression(config.CRON)
      setCronResult(res)
    } catch (err) {
      setCronResult({ valid: false, message: err instanceof Error ? err.message : t('config.networkError') })
    } finally {
      setCronValidating(false)
    }
  }

  const updateField = <K extends keyof ServiceConfig>(key: K, value: ServiceConfig[K]) => {
    if (!config) return
    setConfig({ ...config, [key]: value })
    if (key === 'CRON') setCronResult(null)
  }

  const updateEnvVar = (key: string, value: string) => {
    if (!config) return
    setConfig({ ...config, ENV_VARS: { ...config.ENV_VARS, [key]: value } })
  }

  const addEnvVar = () => {
    if (!config) return
    let base = 'NEW_VAR'
    let name = base
    let i = 1
    while (config.ENV_VARS[name] !== undefined) {
      name = `${base}_${i++}`
    }
    setConfig({ ...config, ENV_VARS: { ...config.ENV_VARS, [name]: '' } })
  }

  const removeEnvVar = (key: string) => {
    if (!config) return
    const envs = { ...config.ENV_VARS }
    delete envs[key]
    setConfig({ ...config, ENV_VARS: envs })
  }

  const msgClass = messageType === 'error'
    ? 'bg-macaron-rose/30 dark:bg-red-900/30 text-red-700 dark:text-red-300'
    : messageType === 'success'
      ? 'bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300'
      : 'bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300'

  return (
    <div>
      <div className="flex items-center gap-4 mb-4">
        <h2 className="text-lg font-semibold text-gray-800 dark:text-gray-100">{t('config.title')}</h2>
        <select
          value={selected}
          aria-label="Select service to configure"
          onChange={(e) => {
            setSelected(e.target.value)
            setMessage(null)
            setIsCreating(false)
          }}
          className="px-3 py-1.5 text-sm border border-macaron-peach dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200 rounded-lg bg-white focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
        >
          <option value="">{t('config.selectService')}</option>
          {services.map((s) => (
            <option key={s.name} value={s.name}>{s.name}</option>
          ))}
        </select>
        <button
          onClick={() => {
            setSelected('')
            setCreatingName('')
            setConfig({
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
            })
            setOriginalConfig(null)
            setIsCreating(true)
            setMessage(null)
          }}
          className="px-3 py-1.5 text-sm bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 rounded-lg hover:bg-blue-200 dark:hover:bg-blue-900/60 transition-colors"
          aria-label="Create new service"
        >
          {t('config.new')}
        </button>
        {selected && !isCreating && (
          <button
            onClick={handleDelete}
            className="px-3 py-1.5 text-sm bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 rounded-lg hover:bg-red-200 dark:hover:bg-red-900/60 transition-colors"
            aria-label={`Delete service ${selected}`}
          >
            {t('config.delete')}
          </button>
        )}
      </div>

      {config ? (
        <div className="bg-white dark:bg-gray-800 rounded-xl border border-macaron-peach/60 dark:border-gray-700 p-4 space-y-3">
          {isCreating && (
            <div>
              <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
                {t('config.serviceName')} <span className="text-red-500">*</span>
              </label>
              <input
                type="text"
                value={creatingName}
                onChange={(e) => setCreatingName(e.target.value)}
                className={inputClass}
                placeholder={t('config.serviceNamePlaceholder')}
              />
            </div>
          )}
          <div>
            <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
              EXEC_CMD <span className="text-gray-400">({t('config.command')})</span>
            </label>
            <input
              type="text"
              value={config.EXEC_CMD}
              onChange={(e) => updateField('EXEC_CMD', e.target.value)}
              className={inputClass}
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
              RESTART_CMD <span className="text-gray-400">({t('config.gracefulReload')})</span>
            </label>
            <input
              type="text"
              value={config.RESTART_CMD || ''}
              onChange={(e) => updateField('RESTART_CMD', e.target.value)}
              className={inputClass}
              placeholder="e.g. nginx -s reload"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
              PID_FILE <span className="text-gray-400">({t('config.pidFile')})</span>
            </label>
            <input
              type="text"
              value={config.PID_FILE || ''}
              onChange={(e) => updateField('PID_FILE', e.target.value)}
              className={inputClass}
              placeholder="e.g. /var/run/nginx.pid"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
              PRE_ACTION <span className="text-gray-400">({t('config.preAction')})</span>
            </label>
            <input
              type="text"
              value={config.PRE_ACTION || ''}
              onChange={(e) => updateField('PRE_ACTION', e.target.value)}
              className={inputClass}
              placeholder="e.g. pkill -f myservice"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
              WORK_DIR <span className="text-gray-400">({t('config.workingDir')})</span>
            </label>
            <input
              type="text"
              value={config.WORK_DIR}
              onChange={(e) => updateField('WORK_DIR', e.target.value)}
              className={inputClass}
            />
          </div>

          <div className="grid grid-cols-3 gap-3">
            <div>
              <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
                RESTART_POLICY <span className="text-gray-400">({t('config.restartPolicy')})</span>
              </label>
              <select
                value={config.RESTART_POLICY}
                onChange={(e) => updateField('RESTART_POLICY', e.target.value)}
                className="w-full px-3 py-1.5 text-sm border border-macaron-peach dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200 rounded-lg bg-white focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
              >
                <option value="always">always</option>
                <option value="on-failure">on-failure</option>
                <option value="never">never</option>
              </select>
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
                CHECK_PORT <span className="text-gray-400">(0 = {t('config.off')})</span>
              </label>
              <input
                type="number"
                value={config.CHECK_PORT || 0}
                onChange={(e) => updateField('CHECK_PORT', parseInt(e.target.value) || 0)}
                className={inputClass}
                placeholder="0"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
                BACK_OFF <span className="text-gray-400">({t('config.backOff')})</span>
              </label>
              <input
                type="number"
                value={config.BACK_OFF}
                onChange={(e) => updateField('BACK_OFF', parseInt(e.target.value) || 0)}
                className={inputClass}
              />
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
                STDOUT <span className="text-gray-400">({t('config.logOutput')})</span>
              </label>
              <input
                type="text"
                value={config.STDOUT}
                onChange={(e) => updateField('STDOUT', e.target.value)}
                className={inputClass}
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
                STDERR <span className="text-gray-400">({t('config.errorOutput')})</span>
              </label>
              <input
                type="text"
                value={config.STDERR}
                onChange={(e) => updateField('STDERR', e.target.value)}
                className={inputClass}
              />
            </div>
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
              CRON <span className="text-gray-400">({t('config.schedule')})</span>
            </label>
            <div className="flex gap-2">
              <input
                type="text"
                value={config.CRON || ''}
                onChange={(e) => updateField('CRON', e.target.value)}
                className="flex-1 px-3 py-1.5 text-sm border border-macaron-peach dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
                placeholder={t('config.cronPlaceholder')}
              />
              <button
                onClick={handleValidateCron}
                disabled={!config.CRON || cronValidating}
                className="px-3 py-1.5 text-xs bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 rounded-lg hover:bg-amber-200 dark:hover:bg-amber-900/60 transition-colors disabled:opacity-40 whitespace-nowrap"
              >
                {cronValidating ? t('config.checking') : t('config.validate')}
              </button>
            </div>
            {cronResult && (
              <div className={`mt-1 text-xs rounded px-2 py-1 ${cronResult.valid ? 'bg-emerald-50 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300' : 'bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-300'}`}>
                {cronResult.valid
                  ? t('config.cronValid', { next1: cronResult.nextRun || '', next2: cronResult.nextRun2 || '' })
                  : t('config.cronInvalid', { message: cronResult.message })
                }
              </div>
            )}
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
              DEPENDS_ON <span className="text-gray-400">({t('config.dependencies')})</span>
            </label>
            <input
              type="text"
              value={config.DEPENDS_ON.join(', ')}
              onChange={(e) => updateField('DEPENDS_ON', e.target.value.split(',').map((s: string) => s.trim()).filter(Boolean))}
              className={inputClass}
              placeholder={t('config.depsPlaceholder')}
            />
          </div>

          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="block text-xs font-medium text-gray-500 dark:text-gray-400">
                ENV_VARS <span className="text-gray-400">({t('config.environment')})</span>
              </label>
              <button onClick={addEnvVar} className="text-xs text-blue-700 dark:text-blue-400 hover:text-blue-900 dark:hover:text-blue-300">
                {t('config.add')}
              </button>
            </div>
            <div className="space-y-1">
              {Object.entries(config.ENV_VARS).map(([key, value]) => (
                <div key={key} className="flex gap-2">
                  <input
                    type="text"
                    value={key}
                    onChange={(e) => {
                      const newKey = e.target.value
                      if (newKey !== key && config.ENV_VARS[newKey] !== undefined) return
                      const envs = { ...config.ENV_VARS }
                      delete envs[key]
                      envs[newKey] = value
                      updateField('ENV_VARS', envs)
                    }}
                    className="w-1/3 px-2 py-1 text-sm border border-macaron-peach dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
                    placeholder={t('config.key')}
                  />
                  <input
                    type="text"
                    value={value}
                    onChange={(e) => updateEnvVar(key, e.target.value)}
                    className="flex-1 px-2 py-1 text-sm border border-macaron-peach dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
                    placeholder={t('config.value')}
                  />
                  <button onClick={() => removeEnvVar(key)} className="text-xs text-red-700 dark:text-red-400 hover:text-red-900 dark:hover:text-red-300 px-1" aria-label={`Remove environment variable ${key}`}>x</button>
                </div>
              ))}
              {Object.keys(config.ENV_VARS).length === 0 && (
                <p className="text-xs text-gray-400 dark:text-gray-500">{t('config.noEnvVars')}</p>
              )}
            </div>
          </div>

          {message && (
            <div className={`text-sm rounded-lg px-3 py-2 ${msgClass}`}>
              {message}
            </div>
          )}

          <div className="flex items-center gap-3 pt-2">
            {isCreating ? (
              <button
                onClick={handleCreate}
                disabled={!creatingName.trim() || !config?.EXEC_CMD?.trim() || saving}
                className="px-4 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white text-sm rounded-lg transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
              >
                {saving ? t('config.creating') : t('config.createService')}
              </button>
            ) : (
              <button
                onClick={handleSave}
                disabled={!hasChanges || saving}
                className="px-4 py-1.5 bg-blue-600 hover:bg-blue-700 text-white text-sm rounded-lg transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
              >
                {saving ? t('config.applying') : t('config.apply')}
              </button>
            )}
            <button
              onClick={handleSaveToFile}
              disabled={saving}
              className="px-4 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white text-sm rounded-lg transition-colors disabled:opacity-40"
            >
              {saving ? t('config.saving') : t('config.saveToFile')}
            </button>
            {isCreating && (
              <button
                onClick={() => {
                  setIsCreating(false)
                  setCreatingName('')
                  setSelected('')
                  setConfig(null)
                  setOriginalConfig(null)
                  setMessage(null)
                }}
                className="px-4 py-1.5 text-sm text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
              >
                {t('config.cancel')}
              </button>
            )}
          </div>
        </div>
      ) : (
        <div className="bg-white dark:bg-gray-800 rounded-xl border border-macaron-peach/60 dark:border-gray-700 p-4">
          <p className="text-sm text-gray-400 dark:text-gray-500">{t('config.selectHint')}</p>
        </div>
      )}
    </div>
  )
}
