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
 * React Query cache keys for access-token audit logs.
 *
 * `allFor(userId)` 对应用户维度全部 scope 的审计缓存：
 * 拉取最新资料前后按它 cancel/remove，避免旧资料覆盖新写入的审计。
 */
export const auditQueryKeys = {
  /** 某用户某 scope 的审计日志 */
  detail: (
    userId: string | number | undefined,
    scope: string,
    params: unknown
  ) => ['audit', userId, scope, params] as const,
  /** 某用户全部 scope 的审计日志前缀 */
  allFor: (userId: string | number | undefined) =>
    ['audit', userId, 'all'] as const,
  /** 访问令牌权限目录（审计详情展开用） */
  accessTokenScopes: ['access-token-scopes'] as const,
}
