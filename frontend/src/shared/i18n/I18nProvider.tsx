import { ReactNode, useEffect, useMemo, useState } from "react"

import { I18nContext, I18nContextValue } from "./context"
import { Language, messages, supportedLanguages } from "./messages"

const STORAGE_KEY = "openlist2emby.language"

function isLanguage(value: string | null | undefined): value is Language {
  return Boolean(value && supportedLanguages.includes(value as Language))
}

function detectLanguage(): Language {
  const saved = window.localStorage.getItem(STORAGE_KEY)
  if (isLanguage(saved)) {
    return saved
  }

  const browserLanguage = window.navigator.language.toLowerCase()
  return browserLanguage.startsWith("zh") ? "zh" : "en"
}

function interpolate(
  template: string,
  values: Record<string, string | number> = {},
) {
  return template.replace(/\{(\w+)\}/g, (_, key: string) =>
    Object.prototype.hasOwnProperty.call(values, key) ? String(values[key]) : `{${key}}`,
  )
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [language, setLanguageState] = useState<Language>(() => detectLanguage())

  useEffect(() => {
    window.localStorage.setItem(STORAGE_KEY, language)
    document.documentElement.lang = language === "zh" ? "zh-CN" : "en"
  }, [language])

  const value = useMemo<I18nContextValue>(() => {
    const dictionary = messages[language]
    return {
      language,
      locale: language === "zh" ? "zh-CN" : "en-US",
      setLanguage: setLanguageState,
      t: (key, values) => interpolate(dictionary[key] ?? messages.en[key], values),
    }
  }, [language])

  return (
    <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
  )
}
