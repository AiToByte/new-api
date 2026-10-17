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
import { defineConfig } from '@playwright/test'

// 生产产物冒烟专用配置：与主 e2e 配置分离，避免 dev webServer 与 dist 静态服务器互相干扰。
// 它加载 `bun run build` 的产物，用于验证生产构建路径（含 en-slim loader 与分包策略）。
const probeUrl = process.env.PROBE_URL ?? 'http://localhost:3200'

export default defineConfig({
  testDir: './e2e',
  testMatch: /dist-smoke\.spec\.ts/,
  fullyParallel: false,
  timeout: 90000,
  expect: {
    // 产物站点首屏按需加载语言块，给足等待时间。
    timeout: 30000,
  },
  retries: process.env.CI ? 2 : 0,
  reporter: 'line',
  use: {
    baseURL: probeUrl,
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'node scripts/serve-dist.mjs',
    url: probeUrl,
    reuseExistingServer: true,
    timeout: 60000,
    env: {
      PROBE_PORT: new URL(probeUrl).port,
      // CI 指向 scratch 后端；本地默认 :3000。
      SERVER_URL: process.env.E2E_API_URL ?? 'http://localhost:3000',
    },
  },
})
