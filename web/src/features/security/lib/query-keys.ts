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
 * React Query cache keys for personal access tokens.
 *
 * `list` 与 `catalog` 分别对应用户维度的令牌列表与权限目录；
 * 创建 / 吊销后 invalidate `list`。
 */
export const accessTokensQueryKeys = {
  list: (userId: string | number | undefined) =>
    ['security', 'access-tokens', 'list', userId] as const,
  catalog: (userId: string | number | undefined) =>
    ['security', 'access-tokens', 'catalog', userId] as const,
}
