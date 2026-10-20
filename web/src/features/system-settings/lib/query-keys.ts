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
/**
 * React Query cache keys scoped to system-settings features.
 */
export const systemSettingsQueryKeys = {
  /** 当前请求策略配置（routing 段），保存后失效 */
  requestPolicy: ['request-policy'] as const,
  /** 自定义 OAuth 提供方列表 */
  customOAuthProviders: ['custom-oauth-providers'] as const,
}
