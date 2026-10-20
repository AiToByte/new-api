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
 * React Query cache keys for usage logs.
 *
 * `all`（`['logs']`）是日志列表的前缀：日志表按完整参数缓存，
 * 而各筛选栏的刷新/等待态只认 `all` 前缀，invalidate `all` 即全量刷新。
 */
export const logsQueryKeys = {
  all: ['logs'] as const,
  /** 日志列表（分类、可见范围、分页、筛选、语言） */
  list: (
    logCategory: string,
    viewAccess: unknown,
    page: number,
    pageSize: number,
    columnFilters: unknown,
    searchParams: unknown,
    t: unknown
  ) =>
    [
      'logs',
      logCategory,
      viewAccess,
      page,
      pageSize,
      columnFilters,
      searchParams,
      t,
    ] as const,
  /** 日志统计摘要 */
  stats: ['usage-logs-stats'] as const,
  statsFor: (isAdmin: boolean, searchParams: unknown) =>
    ['usage-logs-stats', isAdmin, searchParams] as const,
  /** 日志计费来源（按管理员身份与用户） */
  billingSource: (isAdmin: boolean, userId: string | number | undefined) =>
    ['usage-log-billing-source', isAdmin, userId] as const,
  /** 任务产物（任务日志详情中的产物面板） */
  taskArtifacts: (taskId: string | number) =>
    ['usage-logs', 'task-artifacts', taskId] as const,
}
