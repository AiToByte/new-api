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
import { createRequire } from 'node:module'

import i18next from 'i18next'
import { describe, expect, it } from 'vitest'

const require = createRequire(import.meta.url)
const { slimEnglishLocale } = require('../en-slim-loader.cjs') as {
  slimEnglishLocale: (locale: unknown) => {
    translation: Record<string, unknown>
  }
}

describe('slimEnglishLocale', () => {
  it('drops identity entries and keeps non-identity mappings', () => {
    const slim = slimEnglishLocale({
      translation: {
        'Sign in': 'Sign in',
        'Hello {{name}}': 'Hello {{name}}',
        '，and ': ', and ',
      },
    })
    expect(slim.translation).toEqual({ '，and ': ', and ' })
  })

  it('keeps i18next output identical after slimming', async () => {
    const instance = i18next.createInstance()
    await instance.init({
      resources: {
        en: slimEnglishLocale({
          translation: {
            'Plain key': 'Plain key',
            'Hello {{name}}': 'Hello {{name}}',
            '，and ': ', and ',
          },
        }),
      },
      fallbackLng: 'en',
      nsSeparator: false,
      interpolation: { escapeValue: false },
    })
    expect(instance.t('Plain key')).toBe('Plain key')
    expect(instance.t('Hello {{name}}', { name: 'Alex' })).toBe('Hello Alex')
    expect(instance.t('，and ')).toBe(', and ')
  })

  it('falls back to the English key for locales missing a translation', async () => {
    const instance = i18next.createInstance()
    await instance.init({
      resources: {
        en: slimEnglishLocale({
          translation: { 'Only English': 'Only English', '，and ': ', and ' },
        }),
        fr: { translation: {} },
      },
      lng: 'fr',
      fallbackLng: 'en',
      nsSeparator: false,
      interpolation: { escapeValue: false },
    })
    expect(instance.t('Only English')).toBe('Only English')
    expect(instance.t('，and ')).toBe(', and ')
  })
})
