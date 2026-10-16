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

// The rankings page is public and renders bar charts through the shared
// on-demand vchart setup (VChartSimple + RegisteredVChart). If a chart type
// or component registration goes missing, vchart renders nothing instead of
// throwing, so assert canvases actually appear.
test('rankings charts render', async ({ page }) => {
  await page.goto('/rankings')
  const canvases = page.locator('canvas')
  await expect(canvases.first()).toBeVisible({ timeout: 30000 })
  const count = await canvases.count()
  expect(count).toBeGreaterThan(0)
})
