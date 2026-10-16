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
import { describe, expect, it, vi } from 'vitest'

import { guardSectionParam, redirectToDefaultSection } from '../route-config'

class RedirectCapture extends Error {
  options: unknown
  constructor(options: unknown) {
    super('redirect')
    this.options = options
  }
}

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  redirect: (options: unknown) => {
    throw new RedirectCapture(options)
  },
}))

const SECTION_IDS = ['quota', 'currency', 'payment'] as const
const DEFAULT_SECTION = 'quota' as const
const TO = '/system-settings/billing/$section'

function captureRedirect(action: () => void): unknown {
  try {
    action()
  } catch (error) {
    expect(error).toBeInstanceOf(RedirectCapture)
    return (error as RedirectCapture).options
  }
  throw new Error('expected redirect to throw')
}

describe('guardSectionParam', () => {
  it('lets a valid section fall through without redirecting', () => {
    expect(() =>
      guardSectionParam('currency', SECTION_IDS, DEFAULT_SECTION, TO)
    ).not.toThrow()
  })

  it('redirects an unknown section to the default section', () => {
    const options = captureRedirect(() =>
      guardSectionParam('nope', SECTION_IDS, DEFAULT_SECTION, TO)
    )
    expect(options).toEqual({
      to: TO,
      params: { section: DEFAULT_SECTION },
    })
  })

  it('passes replace through when the route replaces history entries', () => {
    const options = captureRedirect(() =>
      guardSectionParam('nope', SECTION_IDS, DEFAULT_SECTION, TO, {
        replace: true,
      })
    )
    expect(options).toEqual({
      to: TO,
      params: { section: DEFAULT_SECTION },
      replace: true,
    })
  })

  it('uses a custom validator when the registry exposes a type guard', () => {
    const isKnown = (section: string) => section.startsWith('known-')
    expect(() =>
      guardSectionParam('unknown-id', SECTION_IDS, DEFAULT_SECTION, TO, {
        isValidSection: isKnown,
      })
    ).toThrow(RedirectCapture)
    expect(() =>
      guardSectionParam('known-id', SECTION_IDS, DEFAULT_SECTION, TO, {
        isValidSection: isKnown,
      })
    ).not.toThrow()
  })
})

describe('redirectToDefaultSection', () => {
  it('always redirects an index route to the default section', () => {
    const options = captureRedirect(() =>
      redirectToDefaultSection(DEFAULT_SECTION, TO)
    )
    expect(options).toEqual({
      to: TO,
      params: { section: DEFAULT_SECTION },
    })
  })

  it('passes replace through for history-replacing index routes', () => {
    const options = captureRedirect(() =>
      redirectToDefaultSection(DEFAULT_SECTION, TO, { replace: true })
    )
    expect(options).toEqual({
      to: TO,
      params: { section: DEFAULT_SECTION },
      replace: true,
    })
  })
})
