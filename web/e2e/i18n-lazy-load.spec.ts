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
import { expect, test } from '@playwright/test'

// Lazy locales (perf/web 2f01f5fd2): only English ships in the entry chunk.
// These specs prove in a real browser that the French chunk loads on demand
// and the UI renders French — no stale English, no missing keys.
test('boot with French preset renders French from the lazy chunk', async ({
  page,
}) => {
  await page.addInitScript(() => {
    window.localStorage.setItem('i18nextLng', 'fr')
  })
  const chunkPromise = page.waitForResponse(/locale-fr.*\.js/)
  await page.goto('/')
  await chunkPromise

  await expect(
    page.getByText('Se connecter', { exact: true }).first()
  ).toBeVisible()
  await expect(page.getByText('Sign in', { exact: true })).toHaveCount(0)
})

test('switching English to French loads the chunk then renders French', async ({
  page,
}) => {
  await page.goto('/')
  await expect(page.getByText('Sign in', { exact: true }).first()).toBeVisible()

  await page.getByRole('button', { name: 'Change language' }).click()
  const chunkPromise = page.waitForResponse(/locale-fr.*\.js/)
  await page.getByRole('menuitem', { name: 'Français' }).click()
  await chunkPromise

  await expect(
    page.getByText('Se connecter', { exact: true }).first()
  ).toBeVisible()
  await expect(page.getByText('Sign in', { exact: true })).toHaveCount(0)
})
