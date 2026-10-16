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

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  timeout: 90000,
  expect: {
    // Dev-server first-hit compile can take a while; CI runs the same way.
    timeout: 30000,
  },
  retries: process.env.CI ? 2 : 0,
  reporter: 'line',
  use: {
    baseURL: 'http://localhost:3100',
    trace: 'retain-on-failure',
  },
  webServer: [
    {
      command: 'bun run dev -- --port 3100',
      url: 'http://localhost:3100',
      reuseExistingServer: true,
      timeout: 180000,
      // CI points the whole harness at its scratch backend; locally this
      // equals the dev default (production :3000 proxy).
      env: {
        VITE_REACT_APP_SERVER_URL:
          process.env.E2E_API_URL ?? 'http://localhost:3000',
      },
    },
    {
      // Captcha specs run against the scratch backend (:3002), never
      // production (:3000). CaptchaEnabled is toggled on that instance only.
      command: 'bun run dev -- --port 3101',
      url: 'http://localhost:3101',
      reuseExistingServer: true,
      timeout: 180000,
      env: {
        VITE_REACT_APP_SERVER_URL:
          process.env.E2E_SCRATCH_API_URL ?? 'http://localhost:3002',
      },
    },
  ],
})
