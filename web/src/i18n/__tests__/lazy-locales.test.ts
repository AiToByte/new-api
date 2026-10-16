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
import { afterEach, describe, expect, it } from 'vitest'

import i18n, { ensureLocaleLoaded } from '@/i18n/config'

afterEach(() => {
  for (const lng of ['fr', 'zhCN']) {
    if (i18n.hasResourceBundle(lng, 'translation')) {
      i18n.removeResourceBundle(lng, 'translation')
    }
  }
})

describe('ensureLocaleLoaded', () => {
  it('loads the French bundle on demand so keys resolve to real translations', async () => {
    // Arrange: French is not in the entry chunk.
    expect(i18n.hasResourceBundle('fr', 'translation')).toBe(false)

    // Act.
    await ensureLocaleLoaded('fr')

    // Assert: bundle present and key resolves to French, not the key itself.
    expect(i18n.hasResourceBundle('fr', 'translation')).toBe(true)
    expect(i18n.t('Sign in', { lng: 'fr' })).toBe('Se connecter')
  })

  it('maps a BCP-47 tag to the interface code before loading', async () => {
    // Arrange & Act: browsers report zh-CN, the project uses zhCN.
    await ensureLocaleLoaded('zh-CN')

    // Assert.
    expect(i18n.hasResourceBundle('zhCN', 'translation')).toBe(true)
    expect(i18n.t('Sign in', { lng: 'zhCN' })).not.toBe('Sign in')
  })

  it('leaves unknown languages untouched so the English fallback applies', async () => {
    // Arrange & Act: must never throw or install a bundle.
    await ensureLocaleLoaded('xx-unknown')

    // Assert.
    expect(i18n.hasResourceBundle('xx-unknown', 'translation')).toBe(false)
    expect(i18n.t('Sign in', { lng: 'xx-unknown' })).toBe('Sign in')
  })

  it('reuses an already loaded bundle on repeated calls', async () => {
    // Arrange.
    await ensureLocaleLoaded('fr')

    // Act & Assert: second call resolves without refetching or throwing.
    await expect(ensureLocaleLoaded('fr')).resolves.toBeUndefined()
    expect(i18n.t('Sign in', { lng: 'fr' })).toBe('Se connecter')
  })
})
