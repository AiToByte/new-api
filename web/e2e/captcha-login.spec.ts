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
import { expect, type Page, test } from '@playwright/test'

import { solveSlideX } from './captcha-solver'

// Captcha login E2E (scratch backend only: BASE_URL below must never point
// at production). Covers the full human path the unit tests cannot: fetch a
// real jigsaw challenge, drag the tile with a real pointer gesture, submit
// the proof, and land in the app. Registration runs first so the spec needs
// no pre-seeded users; both forms share one solver + drag helper.
const SCRATCH_URL = 'http://localhost:3101'
const PASSWORD = 'E2e!test1234'

type SlideChallenge = {
  slide_id: string
  master_image: string
  tile_image: string
  image_width: number
  tile_y: number
}

function collectChallenges(page: Page): Array<SlideChallenge> {
  const seen: Array<SlideChallenge> = []
  page.on('response', (r) => {
    if (r.url().includes('/api/user/captcha/slide') && r.ok()) {
      void r
        .json()
        .then((j) => seen.push(j.data as SlideChallenge))
        .catch(() => undefined)
    }
  })
  return seen
}

// Solve the live challenge and drag the tile. Refreshes (bounded) when the
// solver reports low confidence instead of submitting a doomed proof.
async function solveAndDrag(
  page: Page,
  seen: Array<SlideChallenge>
): Promise<void> {
  for (let attempt = 0; attempt < 3; attempt++) {
    await page.waitForTimeout(3500)
    const data = seen.at(-1)
    expect(data, 'a slide challenge must have been issued').toBeDefined()
    const solved = await solveSlideX(page, data)
    if (solved.score < 0.85) {
      const slider = page.getByRole('slider')
      const box = await slider.first().boundingBox()
      expect(box, 'slider track must be measurable').not.toBeNull()
      const track = box as NonNullable<typeof box>
      const startX = track.x + 2
      const targetClientX = startX + (solved.x / data.image_width) * track.width
      const cy = track.y + track.height / 2
      await page.mouse.move(startX, cy)
      await page.mouse.down()
      await page.mouse.move(targetClientX, cy, { steps: 25 })
      await page.mouse.up()
      await expect(
        page.getByText('Verification passed', { exact: true }).first()
      ).toBeVisible()
      return
    }
    await page.getByRole('button', { name: 'Refresh image captcha' }).click()
  }
  throw new Error('solver stayed ambiguous across 3 challenges')
}

test('slider captcha: register a fresh user then log in', async ({ page }) => {
  const username = `e2e-cap-${Date.now().toString(36)}`
  const seen = collectChallenges(page)

  // --- Registration (slide proof on the sign-up form). ---
  await page.goto(`${SCRATCH_URL}/sign-up`)
  await page.getByRole('slider').first().waitFor({ state: 'visible' })
  await solveAndDrag(page, seen)
  await page.getByPlaceholder('Enter your username').fill(username)
  await page.getByPlaceholder(/enter password/i).fill(PASSWORD)
  await page.getByPlaceholder('Confirm password').fill(PASSWORD)
  await page.getByRole('button', { name: /sign up|create account/i }).click()
  await expect(
    page.getByText('Account created! Please sign in', { exact: true })
  ).toBeVisible()

  // --- Login with the new credentials (fresh challenge, fresh proof). ---
  await page.goto(`${SCRATCH_URL}/sign-in`)
  await page.getByRole('slider').first().waitFor({ state: 'visible' })
  await solveAndDrag(page, seen)
  await page.getByPlaceholder('Enter your username or email').fill(username)
  await page
    .getByPlaceholder(/password/i)
    .first()
    .fill(PASSWORD)
  await page.getByRole('button', { name: /^sign in$/i }).click()
  await expect(page).toHaveURL(/dashboard/)
})
