import { ReactNode, useEffect, useMemo, useState } from "react"

import {
  ResolvedTheme,
  ThemeContext,
  ThemeContextValue,
  ThemePreference,
} from "./context"

export const THEME_STORAGE_KEY = "openlist2emby.theme"
const DARK_MODE_QUERY = "(prefers-color-scheme: dark)"

function isThemePreference(value: string | null): value is ThemePreference {
  return value === "system" || value === "light" || value === "dark"
}

function readThemePreference(): ThemePreference {
  const savedTheme = window.localStorage.getItem(THEME_STORAGE_KEY)
  return isThemePreference(savedTheme) ? savedTheme : "system"
}

function readSystemTheme(): ResolvedTheme {
  return window.matchMedia(DARK_MODE_QUERY).matches ? "dark" : "light"
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<ThemePreference>(() => readThemePreference())
  const [systemTheme, setSystemTheme] = useState<ResolvedTheme>(() => readSystemTheme())
  const resolvedTheme = theme === "system" ? systemTheme : theme

  useEffect(() => {
    const mediaQuery = window.matchMedia(DARK_MODE_QUERY)
    const handleChange = (event: MediaQueryListEvent) => {
      setSystemTheme(event.matches ? "dark" : "light")
    }

    setSystemTheme(mediaQuery.matches ? "dark" : "light")
    mediaQuery.addEventListener("change", handleChange)
    return () => mediaQuery.removeEventListener("change", handleChange)
  }, [])

  useEffect(() => {
    const root = document.documentElement
    root.classList.toggle("dark", resolvedTheme === "dark")
    root.style.colorScheme = resolvedTheme
    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute("content", resolvedTheme === "dark" ? "#18181b" : "#f8fafc")
  }, [resolvedTheme])

  const value = useMemo<ThemeContextValue>(
    () => ({
      theme,
      resolvedTheme,
      setTheme: (nextTheme) => {
        window.localStorage.setItem(THEME_STORAGE_KEY, nextTheme)
        setThemeState(nextTheme)
      },
    }),
    [resolvedTheme, theme],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}
