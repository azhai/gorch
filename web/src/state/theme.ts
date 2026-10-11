export type ColorTheme = 'orange' | 'mint' | 'pinkblue' | 'monet'
export type Mode = 'light' | 'dark'
/** What the theme picker shows: the four light palettes plus a dark scheme. */
export type ThemeOption = ColorTheme | 'dark'

const COLOR_KEY = 'gorch.colorTheme'
const MODE_KEY = 'gorch.mode'

const COLOR_THEMES: readonly ColorTheme[] = ['orange', 'mint', 'pinkblue', 'monet']

function readInitialColorTheme(): ColorTheme {
  try {
    const saved = localStorage.getItem(COLOR_KEY) as ColorTheme | null
    if (saved && COLOR_THEMES.includes(saved)) return saved
  } catch {
    // ignore
  }
  return 'orange'
}

function readInitialMode(): Mode {
  try {
    const saved = localStorage.getItem(MODE_KEY) as Mode | null
    if (saved === 'light' || saved === 'dark') return saved
  } catch {
    // ignore
  }
  if (typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: dark)').matches) {
    return 'dark'
  }
  return 'light'
}

let colorTheme: ColorTheme = readInitialColorTheme()
let mode: Mode = readInitialMode()
const listeners = new Set<() => void>()

/**
 * Material 3 themes are expressed entirely through colour roles, so switching
 * themes only means pointing `data-theme` at a different token block in
 * index.css — no class juggling and no re-render of the tree.
 */
function applyToDocument(): void {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  root.dataset.theme = mode === 'dark' ? 'dark' : colorTheme
  root.style.colorScheme = mode === 'dark' ? 'dark' : 'light'
}

function persist(): void {
  try {
    localStorage.setItem(COLOR_KEY, colorTheme)
    localStorage.setItem(MODE_KEY, mode)
  } catch {
    // ignore
  }
}

function commit(): void {
  persist()
  applyToDocument()
  listeners.forEach((fn) => fn())
}

applyToDocument()

export const theme = {
  get colorTheme(): ColorTheme {
    return colorTheme
  },
  get mode(): Mode {
    return mode
  },
  /** Currently selected picker entry. */
  get option(): ThemeOption {
    return mode === 'dark' ? 'dark' : colorTheme
  },

  setOption(next: ThemeOption): void {
    if (next === 'dark') {
      if (mode === 'dark') return
      mode = 'dark'
    } else {
      const changed = mode === 'dark' || colorTheme !== next
      mode = 'light'
      colorTheme = next
      if (!changed) return
    }
    commit()
  },

  setColorTheme(next: ColorTheme): void {
    if (colorTheme === next) return
    colorTheme = next
    commit()
  },

  setMode(next: Mode): void {
    if (mode === next) return
    mode = next
    commit()
  },

  toggleMode(): void {
    mode = mode === 'light' ? 'dark' : 'light'
    commit()
  },
}

/** Subscribe to theme changes (needed so the picker reflects the current value). */
export function onThemeChange(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

export { COLOR_THEMES }