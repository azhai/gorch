import { translations, LANGS } from './translations.js'

const STORAGE_KEY = 'gorch.lang'

export const LANG_OPTIONS = [
  { value: 'en', label: 'English' },
  { value: 'zh-CN', label: '简体中文' },
]

function readInitialLang() {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved && LANGS.includes(saved)) return saved
  } catch {
    // localStorage unavailable (private mode, embedded webview, …)
  }
  const nav = (typeof navigator !== 'undefined' ? navigator.language : 'en').toLowerCase()
  return nav.startsWith('zh') ? 'zh-CN' : 'en'
}

let current = readInitialLang()
const listeners = new Set()

function notify() {
  listeners.forEach((fn) => fn())
}

/**
 * `vars` interpolates `{name}` placeholders. A string in the second position is
 * treated as a fallback for missing keys.
 */
function translate(lang, key, vars, maybeFallback) {
  const dict = translations[lang]
  const found = (dict && dict[key]) ?? translations.en[key]
  if (found === undefined) {
    return typeof vars === 'string' ? vars : (maybeFallback ?? key)
  }
  if (typeof vars === 'object' && vars !== null) {
    let text = found
    for (const [k, v] of Object.entries(vars)) {
      text = text.replace(`{${k}}`, String(v))
    }
    return text
  }
  return found
}

export const i18n = {
  get lang() {
    return current
  },

  setLang(next) {
    if (next === current) return
    current = next
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // ignore persistence failures
    }
    notify()
  },

  t(key, vars, maybeFallback) {
    return translate(current, key, vars, maybeFallback)
  },
}

export function onLangChange(fn) {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}
