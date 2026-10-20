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
 * React Query cache keys for system monitoring panels (instances, tasks).
 *
 * `taskHistory` 是任务历史列表前缀；`taskHistoryList` 带筛选与分页。
 */
export const systemInfoQueryKeys = {
  /** 实例列表 */
  instances: ['system-info', 'instances'] as const,
  /** 活跃系统任务 */
  systemTasksActive: ['system-info', 'system-tasks', 'active'] as const,
  /** 任务历史（前缀） */
  taskHistory: ['system-info', 'system-tasks', 'history'] as const,
  /** 任务历史（带筛选与分页） */
  taskHistoryList: (filters: unknown, pagination: unknown) =>
    [...systemInfoQueryKeys.taskHistory, filters, pagination] as const,
}
