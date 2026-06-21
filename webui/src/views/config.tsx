import { useState, useEffect } from 'react'
import { fetchServices, fetchServiceConfig, updateServiceConfig, ServiceStatus, ServiceConfig } from '../api/client'

export default function Config() {
  const [services, setServices] = useState<ServiceStatus[]>([])
  const [selected, setSelected] = useState<string>('')
  const [config, setConfig] = useState<ServiceConfig | null>(null)
  const [originalConfig, setOriginalConfig] = useState<ServiceConfig | null>(null)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<string | null>(null)

  useEffect(() => {
    fetchServices().then(setServices)
  }, [])

  useEffect(() => {
    if (!selected) {
      setConfig(null)
      setOriginalConfig(null)
      return
    }
    fetchServiceConfig(selected).then((cfg) => {
      cfg.DEPENDS_ON = cfg.DEPENDS_ON || []
      cfg.ENV_VARS = cfg.ENV_VARS || {}
      setConfig(cfg)
      setOriginalConfig(JSON.parse(JSON.stringify(cfg)))
    })
  }, [selected])

  const hasChanges = config && originalConfig && JSON.stringify(config) !== JSON.stringify(originalConfig)

  const handleSave = async () => {
    if (!config || !selected) return
    setSaving(true)
    setMessage(null)
    try {
      const res = await updateServiceConfig(selected, config)
      if (res.success) {
        setOriginalConfig(JSON.parse(JSON.stringify(config)))
        setMessage('Config updated')
      } else {
        setMessage(res.message || 'Update failed')
      }
    } catch {
      setMessage('Network error')
    } finally {
      setSaving(false)
    }
  }

  const updateField = <K extends keyof ServiceConfig>(key: K, value: ServiceConfig[K]) => {
    if (!config) return
    setConfig({ ...config, [key]: value })
  }

  const updateEnvVar = (key: string, value: string) => {
    if (!config) return
    setConfig({ ...config, ENV_VARS: { ...config.ENV_VARS, [key]: value } })
  }

  const addEnvVar = () => {
    if (!config) return
    setConfig({ ...config, ENV_VARS: { ...config.ENV_VARS, '': '' } })
  }

  const removeEnvVar = (key: string) => {
    if (!config) return
    const envs = { ...config.ENV_VARS }
    delete envs[key]
    setConfig({ ...config, ENV_VARS: envs })
  }

  return (
    <div>
      <div className="flex items-center gap-4 mb-4">
        <h2 className="text-lg font-semibold text-gray-800">Configuration</h2>
        <select
          value={selected}
          onChange={(e) => { setSelected(e.target.value); setMessage(null) }}
          className="px-3 py-1.5 text-sm border border-macaron-peach rounded-lg bg-white focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
        >
          <option value="">Select service...</option>
          {services.map((s) => (
            <option key={s.name} value={s.name}>{s.name}</option>
          ))}
        </select>
      </div>

      {config ? (
        <div className="bg-white rounded-xl border border-macaron-peach/60 p-4 space-y-3">
          <div>
            <label className="block text-xs font-medium text-gray-500 mb-1">EXEC_CMD</label>
            <input
              type="text"
              value={config.EXEC_CMD}
              onChange={(e) => updateField('EXEC_CMD', e.target.value)}
              className="w-full px-3 py-1.5 text-sm border border-macaron-peach rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-500 mb-1">WORK_DIR</label>
            <input
              type="text"
              value={config.WORK_DIR}
              onChange={(e) => updateField('WORK_DIR', e.target.value)}
              className="w-full px-3 py-1.5 text-sm border border-macaron-peach rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
            />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-gray-500 mb-1">RESTART_POLICY</label>
              <select
                value={config.RESTART_POLICY}
                onChange={(e) => updateField('RESTART_POLICY', e.target.value)}
                className="w-full px-3 py-1.5 text-sm border border-macaron-peach rounded-lg bg-white focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
              >
                <option value="always">always</option>
                <option value="on-failure">on-failure</option>
                <option value="never">never</option>
              </select>
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-500 mb-1">BACK_OFF</label>
              <input
                type="number"
                value={config.BACK_OFF}
                onChange={(e) => updateField('BACK_OFF', parseInt(e.target.value) || 0)}
                className="w-full px-3 py-1.5 text-sm border border-macaron-peach rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
              />
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-gray-500 mb-1">STDOUT</label>
              <input
                type="text"
                value={config.STDOUT}
                onChange={(e) => updateField('STDOUT', e.target.value)}
                className="w-full px-3 py-1.5 text-sm border border-macaron-peach rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-500 mb-1">STDERR</label>
              <input
                type="text"
                value={config.STDERR}
                onChange={(e) => updateField('STDERR', e.target.value)}
                className="w-full px-3 py-1.5 text-sm border border-macaron-peach rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
              />
            </div>
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-500 mb-1">CRON</label>
            <input
              type="text"
              value={config.CRON}
              onChange={(e) => updateField('CRON', e.target.value)}
              className="w-full px-3 py-1.5 text-sm border border-macaron-peach rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
              placeholder="e.g. 0 */30 * * * *"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-500 mb-1">DEPENDS_ON</label>
            <input
              type="text"
              value={config.DEPENDS_ON.join(', ')}
              onChange={(e) => updateField('DEPENDS_ON', e.target.value.split(',').map((s: string) => s.trim()).filter(Boolean))}
              className="w-full px-3 py-1.5 text-sm border border-macaron-peach rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
              placeholder="comma-separated service names"
            />
          </div>

          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="block text-xs font-medium text-gray-500">ENV_VARS</label>
              <button onClick={addEnvVar} className="text-xs text-blue-700 hover:text-blue-900">+ Add</button>
            </div>
            <div className="space-y-1">
              {Object.entries(config.ENV_VARS).map(([key, value]) => (
                <div key={key} className="flex gap-2">
                  <input
                    type="text"
                    value={key}
                    onChange={(e) => {
                      const envs = { ...config.ENV_VARS }
                      delete envs[key]
                      envs[e.target.value] = value
                      updateField('ENV_VARS', envs)
                    }}
                    className="w-1/3 px-2 py-1 text-sm border border-macaron-peach rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
                    placeholder="KEY"
                  />
                  <input
                    type="text"
                    value={value}
                    onChange={(e) => updateEnvVar(key, e.target.value)}
                    className="flex-1 px-2 py-1 text-sm border border-macaron-peach rounded-lg focus:outline-none focus:ring-2 focus:ring-macaron-orange/50"
                    placeholder="value"
                  />
                  <button onClick={() => removeEnvVar(key)} className="text-xs text-red-700 hover:text-red-900 px-1">x</button>
                </div>
              ))}
              {Object.keys(config.ENV_VARS).length === 0 && (
                <p className="text-xs text-gray-400">No environment variables</p>
              )}
            </div>
          </div>

          {message && (
            <div className={`text-sm rounded-lg px-3 py-2 ${message === 'Config updated' ? 'bg-emerald-100 text-emerald-700' : 'bg-macaron-rose/30 text-red-700'}`}>
              {message}
            </div>
          )}

          <div className="flex items-center gap-3 pt-2">
            <button
              onClick={handleSave}
              disabled={!hasChanges || saving}
              className="px-4 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white text-sm rounded-lg transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {saving ? 'Saving...' : 'Save'}
            </button>
            <span className="text-xs text-gray-400">Runtime config, lost on restart</span>
          </div>
        </div>
      ) : (
        <div className="bg-white rounded-xl border border-macaron-peach/60 p-4">
          <p className="text-sm text-gray-400">Select a service to edit its configuration</p>
        </div>
      )}
    </div>
  )
}
