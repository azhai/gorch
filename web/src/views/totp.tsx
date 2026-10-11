import m from 'mithril'
import {
  TOTPSetupResponse,
  TOTPStatus,
  totpDisable,
  totpRegenerateBackupCodes,
  totpSetup,
  totpStatus,
  totpVerifySetup,
} from '../api/client'
import { i18n } from '../state/i18n'

const BLANK_STATUS: TOTPStatus = { enabled: false, backupCodes: 0, hasBinding: false }

export const TOTPView: m.Component = (() => {
  const state = {
    status: null as TOTPStatus | null,
    setupData: null as TOTPSetupResponse | null,
    verifyCode: '',
    loading: false,
    error: '',
    success: '',
  }

  async function loadStatus(): Promise<void> {
    state.loading = true
    try {
      const res = await totpStatus()
      state.status = res.success && res.data ? res.data : BLANK_STATUS
    } catch {
      state.error = i18n.t('totp.failedLoad')
    } finally {
      state.loading = false
    }
    m.redraw()
  }

  async function handleSetup(): Promise<void> {
    state.loading = true
    state.error = ''
    state.success = ''
    try {
      const res = await totpSetup()
      if (res.success && res.data) {
        state.setupData = res.data
      } else {
        state.error = res.message || i18n.t('totp.setupFailed')
      }
    } catch {
      state.error = i18n.t('totp.failedSetup')
    } finally {
      state.loading = false
    }
    m.redraw()
  }

  async function handleVerifySetup(): Promise<void> {
    if (!state.verifyCode) {
      state.error = i18n.t('totp.enterCode')
      return
    }
    state.loading = true
    state.error = ''
    try {
      const res = await totpVerifySetup(state.verifyCode)
      if (res.success) {
        state.success = i18n.t('totp.enabledSuccess')
        state.setupData = null
        state.verifyCode = ''
        await loadStatus()
      } else {
        state.error = res.message || i18n.t('totp.verifyFailed')
      }
    } catch {
      state.error = i18n.t('totp.failedVerify')
    } finally {
      state.loading = false
    }
    m.redraw()
  }

  async function handleDisable(): Promise<void> {
    if (!confirm(i18n.t('totp.disableConfirm'))) return
    state.loading = true
    state.error = ''
    try {
      const res = await totpDisable()
      if (res.success) {
        state.success = i18n.t('totp.disabledSuccess')
        await loadStatus()
      } else {
        state.error = res.message || i18n.t('totp.failedDisable')
      }
    } catch {
      state.error = i18n.t('totp.failedDisable')
    } finally {
      state.loading = false
    }
    m.redraw()
  }

  async function handleRegenerateBackupCodes(): Promise<void> {
    if (!confirm(i18n.t('totp.regenerateConfirm'))) return
    state.loading = true
    state.error = ''
    try {
      const res = await totpRegenerateBackupCodes()
      if (res.success && res.data) {
        if (state.setupData) {
          state.setupData = { ...state.setupData, backupCodes: res.data.backupCodes }
        }
        state.success = i18n.t('totp.regenerated')
        await loadStatus()
      } else {
        state.error = res.message || i18n.t('totp.failedRegenerate')
      }
    } catch {
      state.error = i18n.t('totp.failedRegenerate')
    } finally {
      state.loading = false
    }
    m.redraw()
  }

  function renderStatus() {
    const status = state.status
    if (state.loading || !status) {
      return <div class="md-muted-sm">{i18n.t('totp.loading')}</div>
    }

    return (
      <div class="md-field-group">
        <div class="md-card">
          <div class="md-row md-row--between">
            <div>
              <h3>{i18n.t('totp.status')}</h3>
              <div class="md-muted-sm" style="margin-top:4px">
                {status.enabled ? i18n.t('totp.enabled') : i18n.t('totp.notEnabled')}
              </div>
            </div>
            <span class={`md-chip ${status.enabled ? 'md-chip--success' : 'md-chip--filled'}`}>
              {status.enabled ? i18n.t('totp.enabledBadge') : i18n.t('totp.disabledBadge')}
            </span>
          </div>
        </div>

        {status.enabled && (
          <div class="md-card">
            <h3 style="margin-bottom:4px">{i18n.t('totp.backupCodes')}</h3>
            <div class="md-muted-sm" style="margin-bottom:12px">
              {i18n.t('totp.backupRemaining', { count: status.backupCodes })}
            </div>
            <button
              class="md-btn md-btn--tonal md-btn--sm"
              disabled={state.loading}
              onclick={() => handleRegenerateBackupCodes()}
            >
              {i18n.t('totp.regenerate')}
            </button>
          </div>
        )}

        <div class="md-row md-row--tight">
          {!status.enabled && (
            <button class="md-btn md-btn--filled" disabled={state.loading} onclick={() => handleSetup()}>
              {i18n.t('totp.enable')}
            </button>
          )}
          {status.enabled && (
            <button class="md-btn md-btn--danger" disabled={state.loading} onclick={() => handleDisable()}>
              {i18n.t('totp.disable')}
            </button>
          )}
        </div>
      </div>
    )
  }

  function renderSetup() {
    const setup = state.setupData
    if (!setup) return null

    return (
      <div class="md-field-group">
        <div class="md-card" style="text-align:center">
          <h3 style="margin-bottom:16px">{i18n.t('totp.scanQr')}</h3>
          <img class="md-qr" src={setup.qrCode} alt="TOTP QR Code" />
          <div class="md-muted-sm" style="margin-top:12px">
            {i18n.t('totp.scanQrHelp')}
          </div>
        </div>

        <div class="md-card">
          <h3 style="margin-bottom:16px">{i18n.t('totp.manualEntry')}</h3>
          <div class="md-code-box md-mono">{setup.secret}</div>
        </div>

        <div class="md-card">
          <h3 style="margin-bottom:16px">{i18n.t('totp.backupCodes')}</h3>
          <div class="md-muted-sm" style="margin-bottom:12px">
            {i18n.t('totp.backupSaveHint')}
          </div>
          <div class="md-grid-2">
            {setup.backupCodes.map((code, i) => (
              <div key={i} class="md-code-box md-mono" style="text-align:center">
                {code}
              </div>
            ))}
          </div>
        </div>

        <div class="md-card">
          <h3 style="margin-bottom:16px">{i18n.t('totp.verifySetup')}</h3>
          <div class="md-muted-sm" style="margin-bottom:12px">
            {i18n.t('totp.verifySetupHelp')}
          </div>
          <div class="md-row md-row--tight">
            <input
              class="md-field md-centered-code"
              style="flex:1"
              type="text"
              maxlength={6}
              value={state.verifyCode}
              placeholder="000000"
              oninput={(e: Event) => {
                state.verifyCode = (e.target as HTMLInputElement).value.replace(/\D/g, '').slice(0, 6)
              }}
            />
            <button
              class="md-btn md-btn--filled"
              disabled={state.loading || state.verifyCode.length !== 6}
              onclick={() => handleVerifySetup()}
            >
              {i18n.t('totp.verifyButton')}
            </button>
          </div>
        </div>

        <button
          class="md-btn"
          onclick={() => {
            state.setupData = null
            state.verifyCode = ''
          }}
        >
          {i18n.t('totp.cancelSetup')}
        </button>
      </div>
    )
  }

  return {
    oninit() {
      loadStatus()
    },

    view() {
      return (
        <div style="max-width:672px;margin:0 auto">
          <h2 style="margin-bottom:24px">{i18n.t('totp.title')}</h2>

          {state.error && (
            <div class="md-banner md-banner--error" style="margin-bottom:16px">
              {state.error}
            </div>
          )}
          {state.success && (
            <div class="md-banner md-banner--success" style="margin-bottom:16px">
              {state.success}
            </div>
          )}

          {state.setupData ? renderSetup() : renderStatus()}
        </div>
      )
    },
  }
})()
