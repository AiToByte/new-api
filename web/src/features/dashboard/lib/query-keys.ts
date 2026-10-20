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
 * React Query cache keys for the dashboard overview and flow charts.
 */
export const dashboardQueryKeys = {
  /** 用户额度时间线 */
  userQuota: (timeRange: unknown) =>
    ['dashboard', 'user-quota', timeRange] as const,
  /** 概览：用户模型统计 */
  overviewUserModels: ['dashboard', 'overview', 'user-models'] as const,
  /** 概览：API 密钥统计 */
  overviewApiKeys: ['dashboard', 'overview', 'api-keys'] as const,
  /** 概览：迷你趋势图（按起止时间戳） */
  summarySparklines: (startTimestamp: number, endTimestamp: number) =>
    [
      'dashboard',
      'overview',
      'summary-sparklines',
      startTimestamp,
      endTimestamp,
    ] as const,
  /** 流量图（查询参数 + 角色视角） */
  flow: (flowQueryParams: unknown, flowRole: unknown) =>
    ['dashboard', 'flow', flowQueryParams, flowRole] as const,
}
