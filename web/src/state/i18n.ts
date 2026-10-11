import { translations, type Lang } from '../i18n/translations'

export type { Lang }

const STORAGE_KEY = 'gorch.lang'

function readInitialLang(): Lang {
  try {
    const saved = localStorage.getItem(STORAGE_KEY) as Lang | null
    if (saved && saved in translations) return saved
  } catch {
    // localStorage unavailable (private mode, embedded webview, …)
  }
  const nav = (typeof navigator !== 'undefined' ? navigator.language : 'en').toLowerCase()
  return nav.startsWith('zh') ? 'zh-CN' : 'en'
}

let current: Lang = readInitialLang()
const listeners = new Set<() => void>()

function notify(): void {
  listeners.forEach((fn) => fn())
}

/**
 * Translation store. Mithril has no context, so this is a tiny observable
 * singleton: mutate through `i18n.setLang` and callers redraw.
 */
export const i18n = {
  get lang(): Lang {
    return current
  },

  setLang(next: Lang): void {
    if (next === current) return
    current = next
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // ignore persistence failures
    }
    notify()
  },

  /**
   * `vars` interpolates `{name}` placeholders. A string in the second position
   * is treated as a fallback for missing keys, matching the previous API.
   */
  t(
    key: string,
    vars?: Record<string, string | number> | string,
    maybeFallback?: string
  ): string {
    const dict = translations[current]
    const found = dict[key] ?? translations.en[key]
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
  },
}

/** Subscribe to language changes. Returns an unsubscribe function. */
export function onLangChange(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}