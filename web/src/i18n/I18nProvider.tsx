import { createContext, useContext, useState, useEffect, ReactNode, useCallback } from 'react'
import { translations } from './translations'
import type { Lang } from './translations'

export type { Lang }

interface I18nContextValue {
  lang: Lang
  setLang: (lang: Lang) => void
  t: (key: string, varsOrFallback?: Record<string, string | number> | string, fallback?: string) => string
}

const I18nContext = createContext<I18nContextValue | null>(null)

const STORAGE_KEY = 'gorch.lang'

function getInitialLang(): Lang {
  const saved = localStorage.getItem(STORAGE_KEY) as Lang | null
  if (saved && saved in translations) return saved
  const browserLang = navigator.language.toLowerCase()
  if (browserLang.startsWith('zh')) return 'zh-CN'
  return 'en'
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(getInitialLang)

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, lang)
  }, [lang])

  const setLang = useCallback((newLang: Lang) => {
    setLangState(newLang)
  }, [])

  const t = useCallback((key: string, varsOrFallback?: Record<string, string | number> | string, maybeFallback?: string) => {
    const dict = translations[lang]
    const found = dict[key] ?? translations.en[key]
    if (found === undefined) {
      return typeof varsOrFallback === 'string' ? varsOrFallback : maybeFallback ?? key
    }
    const vars = typeof varsOrFallback === 'object' ? varsOrFallback : undefined
    let text = found
    if (vars) {
      for (const [k, v] of Object.entries(vars)) {
        text = text.replace(`{${k}}`, String(v))
      }
    }
    return text
  }, [lang])

  return (
    <I18nContext.Provider value={{ lang, setLang, t }}>
      {children}
    </I18nContext.Provider>
  )
}

export function useI18n() {
  const ctx = useContext(I18nContext)
  if (!ctx) throw new Error('useI18n must be used within I18nProvider')
  return ctx
}
