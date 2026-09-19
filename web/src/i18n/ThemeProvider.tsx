import { createContext, useContext, useState, useEffect, ReactNode, useCallback } from 'react'

export type ColorTheme = 'orange' | 'mint' | 'pinkblue' | 'monet'
export type Mode = 'light' | 'dark'

interface ThemeContextValue {
  colorTheme: ColorTheme
  mode: Mode
  setColorTheme: (theme: ColorTheme) => void
  setMode: (mode: Mode) => void
  toggleMode: () => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

const COLOR_KEY = 'gorch.colorTheme'
const MODE_KEY = 'gorch.mode'

const COLOR_THEME_CLASSES: Record<ColorTheme, string> = {
  orange: '',
  mint: 'theme-mint',
  pinkblue: 'theme-pinkblue',
  monet: 'theme-monet',
}

function getInitialColorTheme(): ColorTheme {
  const saved = localStorage.getItem(COLOR_KEY) as ColorTheme | null
  if (saved && saved in COLOR_THEME_CLASSES) return saved
  return 'orange'
}

function getInitialMode(): Mode {
  const saved = localStorage.getItem(MODE_KEY) as Mode | null
  if (saved === 'light' || saved === 'dark') return saved
  if (window.matchMedia?.('(prefers-color-scheme: dark)').matches) return 'dark'
  return 'light'
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [colorTheme, setColorThemeState] = useState<ColorTheme>(getInitialColorTheme)
  const [mode, setModeState] = useState<Mode>(getInitialMode)

  useEffect(() => {
    const root = document.documentElement
    Object.values(COLOR_THEME_CLASSES).forEach((cls) => {
      if (cls) root.classList.remove(cls)
    })
    const cls = COLOR_THEME_CLASSES[colorTheme]
    if (cls) root.classList.add(cls)
    localStorage.setItem(COLOR_KEY, colorTheme)
  }, [colorTheme])

  useEffect(() => {
    const root = document.documentElement
    if (mode === 'dark') {
      root.classList.add('dark')
    } else {
      root.classList.remove('dark')
    }
    localStorage.setItem(MODE_KEY, mode)
  }, [mode])

  const setColorTheme = useCallback((theme: ColorTheme) => {
    setColorThemeState(theme)
  }, [])

  const setMode = useCallback((newMode: Mode) => {
    setModeState(newMode)
  }, [])

  const toggleMode = useCallback(() => {
    setModeState((prev) => (prev === 'light' ? 'dark' : 'light'))
  }, [])

  return (
    <ThemeContext.Provider value={{ colorTheme, mode, setColorTheme, setMode, toggleMode }}>
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme() {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be used within ThemeProvider')
  return ctx
}
