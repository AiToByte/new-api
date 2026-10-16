/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import i18n from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

import { convertDetectedLanguage, normalizeInterfaceLanguage } from './languages'
import en from './locales/en.json'

// Only English ships in the entry chunk (fallback language, ~540KB saved
// from every first paint). The other six locales load on demand: the detected
// language below, and any later switch via ensureLocaleLoaded (call sites must
// await it BEFORE changeLanguage, otherwise the UI renders the English
// fallback until the next language change).
type LocaleFile = { translation: Record<string, unknown> }

const lazyLocaleLoaders: Record<string, () => Promise<{ default: LocaleFile }>> = {
  zhCN: () => import(/* webpackChunkName: "locale-zh" */ './locales/zh.json'),
  fr: () => import(/* webpackChunkName: "locale-fr" */ './locales/fr.json'),
  ru: () => import(/* webpackChunkName: "locale-ru" */ './locales/ru.json'),
  ja: () => import(/* webpackChunkName: "locale-ja" */ './locales/ja.json'),
  vi: () => import(/* webpackChunkName: "locale-vi" */ './locales/vi.json'),
  zhTW: () => import(/* webpackChunkName: "locale-zh-tw" */ './locales/zh-TW.json'),
}

const pendingLocaleLoads = new Map<string, Promise<void>>()

export function ensureLocaleLoaded(language: string): Promise<void> {
  const code = normalizeInterfaceLanguage(language)
  if (code === 'en' || i18n.hasResourceBundle(code, 'translation')) {
    return Promise.resolve()
  }
  const loader = lazyLocaleLoaders[code]
  if (!loader) return Promise.resolve()
  let pending = pendingLocaleLoads.get(code)
  if (!pending) {
    pending = loader().then(
      (mod) => {
        i18n.addResourceBundle(code, 'translation', mod.default.translation, true, true)
        pendingLocaleLoads.delete(code)
      },
      (err: unknown) => {
        pendingLocaleLoads.delete(code)
        throw err
      }
    )
    pendingLocaleLoads.set(code, pending)
  }
  return pending
}

// Mirror of the detector order (localStorage, navigator) so the first paint
// already carries the right locale instead of flashing English. Runs once as
// top-level await: English users pay nothing, others pay one chunk fetch.
function detectInitialLanguage(): string {
  if (typeof window === 'undefined') return 'en'
  try {
    const stored = window.localStorage.getItem('i18nextLng')
    if (stored) return normalizeInterfaceLanguage(convertDetectedLanguage(stored))
    const nav = window.navigator?.language
    if (nav) return normalizeInterfaceLanguage(convertDetectedLanguage(nav))
  } catch {
    // Storage unavailable (private mode, SSR): fall through to English.
  }
  return 'en'
}

const initialLanguage = detectInitialLanguage()
const initialResources: Record<string, LocaleFile> = { en }
if (initialLanguage !== 'en') {
  try {
    const mod = await lazyLocaleLoaders[initialLanguage]?.()
    if (mod) initialResources[initialLanguage] = mod.default
  } catch {
    // Locale chunk failed: init continues with English, the detector keeps
    // the language and a later ensureLocaleLoaded can retry.
  }
}

await i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: initialResources,
    fallbackLng: 'en',
    supportedLngs: ['en', 'zhCN', 'fr', 'ru', 'ja', 'vi', 'zhTW'],
    load: 'currentOnly',
    nsSeparator: false, // Allow literal colons in keys (e.g., URLs, labels)
    debug: import.meta.env.DEV,
    interpolation: {
      escapeValue: false, // not needed for react as it escapes by default
    },
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
      // Browsers report `zh-CN`/`zh-TW`/`zh`; map them onto our `zhCN`/`zhTW`
      // codes (non-Chinese codes pass through for normal supportedLngs matching).
      convertDetectedLanguage,
    },
  })

export default i18n
