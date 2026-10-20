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
 * 跨特性域共享的 React Query 缓存键。
 *
 * 收录标准：同一键被两个以上特性域读写，或参与 query/invalidate 配对。
 * 集中到全局注册表的目的是让「写入缓存的键」与「失效缓存的键」引用同一处
 * 定义——拼写错误在编译期暴露，而不是让失效操作静默永不命中。
 * 特性域私有的键放在各自 `features/<domain>/lib/query-keys.ts`。
 */
export const globalQueryKeys = {
  /** 系统选项总表（/api/option），设置保存后按此前缀失效 */
  systemOptions: ['system-options'] as const,
  /** 运行状态探针（status 端点），配置类写操作后失效 */
  status: ['status'] as const,
  /** 用户分组列表，被渠道 / 用户 / 日志筛选等多个面板查询 */
  groups: ['groups'] as const,
  /** 令牌可见的用户分组列表 */
  userGroups: ['user-groups'] as const,
  /** 初始化向导完成状态 */
  setupStatus: ['setup-status'] as const,
  /** 模型性能指标（按模型名） */
  perfMetrics: (modelName: string) => ['perf-metrics', modelName] as const,
  /** 模型性能指标汇总（窗口小时数，可选附带模型列表） */
  perfMetricsSummary: (windowHours: number) =>
    ['perf-metrics-summary', windowHours] as const,
  perfMetricsSummaryForModels: (windowHours: number, models: string[]) =>
    ['perf-metrics-summary', windowHours, models] as const,
}
