import { createContext } from "react"

import { Language, TranslationKey } from "./messages"

export interface I18nContextValue {
  language: Language
  locale: string
  setLanguage: (language: Language) => void
  t: (key: TranslationKey, values?: Record<string, string | number>) => string
}

export const I18nContext = createContext<I18nContextValue | null>(null)
