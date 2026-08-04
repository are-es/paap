'use client'

import { createContext, useContext, useState, useEffect, useCallback, type ReactNode } from 'react'
import en from '@/i18n/en.json'
import id from '@/i18n/id.json'
import zh from '@/i18n/zh.json'
import ja from '@/i18n/ja.json'
import ko from '@/i18n/ko.json'
import ar from '@/i18n/ar.json'

const translations: Record<string, Record<string, string>> = { en, id, zh, ja, ko, ar }

export const LANGUAGES = [
  { code: 'en', label: 'English', flag: '🇺🇸' },
  { code: 'id', label: 'Indonesia', flag: '🇮🇩' },
  { code: 'zh', label: '中文', flag: '🇨🇳' },
  { code: 'ja', label: '日本語', flag: '🇯🇵' },
  { code: 'ko', label: '한국어', flag: '🇰🇷' },
  { code: 'ar', label: 'العربية', flag: '🇸🇦' },
] as const

export type LangCode = (typeof LANGUAGES)[number]['code']

interface LanguageContextValue {
  lang: LangCode
  setLang: (lang: LangCode) => void
  t: (key: string, params?: Record<string, string>) => string
}

const STORAGE_KEY = 'paap-language'

const LanguageContext = createContext<LanguageContextValue | null>(null)

export function LanguageProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<LangCode>('en')

  useEffect(() => {
    const stored = localStorage.getItem(STORAGE_KEY) as LangCode | null
    if (stored && translations[stored]) setLangState(stored)
  }, [])

  const setLang = useCallback((code: LangCode) => {
    setLangState(code)
    localStorage.setItem(STORAGE_KEY, code)
    document.documentElement.lang = code
    if (code === 'ar') {
      document.documentElement.dir = 'rtl'
    } else {
      document.documentElement.dir = 'ltr'
    }
  }, [])

  const t = useCallback(
    (key: string, params?: Record<string, string>): string => {
      let value = translations[lang]?.[key] || translations.en[key] || key
      if (params) {
        for (const [k, v] of Object.entries(params)) {
          value = value.replace(`{${k}}`, v)
        }
      }
      return value
    },
    [lang]
  )

  return (
    <LanguageContext.Provider value={{ lang, setLang, t }}>
      {children}
    </LanguageContext.Provider>
  )
}

export function useLanguage() {
  const ctx = useContext(LanguageContext)
  if (!ctx) throw new Error('useLanguage must be used within LanguageProvider')
  return ctx
}
