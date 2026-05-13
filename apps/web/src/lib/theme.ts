import { useEffect, useState } from 'react'

export type ThemePreference = 'system' | 'dark' | 'light'
export type ResolvedTheme = 'dark' | 'light'

const THEME_STORAGE_KEY = 'infinite-ai-theme'

export function normalizeThemePreference(value: unknown): ThemePreference {
  if (value === 'system' || value === 'dark' || value === 'light') return value
  return 'system'
}

export function readThemePreference() {
  if (typeof localStorage !== 'object') return 'system'
  try {
    return normalizeThemePreference(localStorage.getItem(THEME_STORAGE_KEY))
  } catch {
    return 'system'
  }
}

export function saveThemePreference(theme: ThemePreference) {
  if (typeof localStorage !== 'object') return
  try {
    localStorage.setItem(THEME_STORAGE_KEY, theme)
  } catch {}
}

function getSystemTheme(): ResolvedTheme {
  if (typeof window !== 'object') return 'light'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function useResolvedTheme(theme: ThemePreference): ResolvedTheme {
  const [systemTheme, setSystemTheme] = useState<ResolvedTheme>(() => getSystemTheme())
  const resolvedTheme = theme === 'system' ? systemTheme : theme

  useEffect(() => {
    if (typeof window !== 'object') return
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    const updateSystemTheme = () => setSystemTheme(mediaQuery.matches ? 'dark' : 'light')
    updateSystemTheme()
    mediaQuery.addEventListener('change', updateSystemTheme)
    return () => mediaQuery.removeEventListener('change', updateSystemTheme)
  }, [])

  useEffect(() => {
    if (typeof document !== 'object') return
    document.documentElement.dataset.colorScheme = resolvedTheme
    document.documentElement.style.colorScheme = resolvedTheme
  }, [resolvedTheme])

  useEffect(() => {
    saveThemePreference(theme)
  }, [theme])

  return resolvedTheme
}
