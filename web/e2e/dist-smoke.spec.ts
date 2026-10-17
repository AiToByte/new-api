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

// 生产产物冒烟：用真实浏览器加载 `bun run build` 的 dist（由 scripts/serve-dist.mjs 提供）。
// 保护生产专用路径，dev 服务器不覆盖这些行为：
//   - en-slim loader 剥离 en.json 的恒等条目后，界面仍按英文默认值正常渲染；
//   - 缺失的键由 i18next 回退为键本身，因此不应出现未替换的插值占位符；
//   - 语言块仍为按需加载的异步 chunk；
//   - 启动过程没有未捕获的页面异常。
test.describe('production dist smoke', () => {
  test('boots and renders the English default without page errors', async ({
    page,
  }) => {
    const pageErrors: string[] = []
    page.on('pageerror', (error) => pageErrors.push(error.message))

    await page.goto('/')
    await expect(
      page.getByText('Sign in', { exact: true }).first()
    ).toBeVisible()

    // 恒等条目被剥离后，缺失键回退为英文键名；若回退链路破损，
    // 含插值的键会以原始 `{{...}}` 文本泄漏到界面。
    const body = await page.locator('body').innerText()
    expect(body).not.toContain('{{')
    expect(pageErrors).toEqual([])
  })

  test('loads the lazy French chunk on demand', async ({ page }) => {
    await page.goto('/')
    await expect(
      page.getByText('Sign in', { exact: true }).first()
    ).toBeVisible()

    await page.getByRole('button', { name: 'Change language' }).click()
    const chunkPromise = page.waitForResponse(/locale-fr.*\.js/)
    await page.getByRole('menuitem', { name: 'Français' }).click()
    await chunkPromise

    await expect(
      page.getByText('Se connecter', { exact: true }).first()
    ).toBeVisible()
  })
})
