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
import type { GetModelsParams, SearchModelsParams } from '../types'

/**
 * React Query cache keys for models
 */
export const modelsQueryKeys = {
  all: ['models'] as const,
  lists: () => [...modelsQueryKeys.all, 'list'] as const,
  list: (filters: GetModelsParams | SearchModelsParams) =>
    [...modelsQueryKeys.lists(), filters] as const,
  detail: (id: number) => [...modelsQueryKeys.all, 'detail', id] as const,
  missing: () => [...modelsQueryKeys.all, 'missing'] as const,
  /** 渠道标签编辑器用的全量模型列表 */
  allModels: ['all-models'] as const,
  /** 用户可见模型列表（API 密钥抽屉） */
  userModels: ['user-models'] as const,
  /** 用户可见模型列表（Claude Code 切换弹窗） */
  userModelsCcswitch: ['user-models-ccswitch'] as const,
  /** 按标签过滤的模型列表 */
  tagModels: (currentTag: string | null) => ['tag-models', currentTag] as const,
  /** 渠道已配置模型列表 */
  channelModels: ['channel_models'] as const,
  /** 启用态模型列表（倍率表单下拉） */
  enabledModels: ['enabled-models'] as const,
  /** 渠道抽屉里的模型预填分组（历史 snake_case 键，保持原样） */
  modelPrefillGroups: ['prefill_groups', 'model'] as const,
}

/**
 * React Query cache keys for vendors
 */
export const vendorsQueryKeys = {
  all: ['vendors'] as const,
  lists: () => [...vendorsQueryKeys.all, 'list'] as const,
  list: (filters?: Record<string, unknown>) =>
    [...vendorsQueryKeys.lists(), filters] as const,
  detail: (id: number) => [...vendorsQueryKeys.all, 'detail', id] as const,
}

/**
 * React Query cache keys for prefill groups
 */
export const prefillGroupsQueryKeys = {
  all: ['prefill-groups'] as const,
  lists: () => [...prefillGroupsQueryKeys.all, 'list'] as const,
  list: (type?: string) => [...prefillGroupsQueryKeys.lists(), type] as const,
}

/**
 * React Query cache keys for deployments
 */
export const deploymentsQueryKeys = {
  all: ['deployments'] as const,
  lists: () => [...deploymentsQueryKeys.all, 'list'] as const,
  list: (filters: {
    keyword?: string
    status?: string
    p?: number
    page_size?: number
  }) => [...deploymentsQueryKeys.lists(), filters] as const,
  detail: (id: string | number) =>
    [...deploymentsQueryKeys.all, 'detail', id] as const,
}

/**
 * React Query cache keys for the ionet deployment detail dialogs.
 *
 * 与 `deploymentsQueryKeys`（部署管理列表页）是两套键：
 * 这里的 `all` 是各详情弹窗共用的失效前缀——重命名 / 改配置 /
 * 续费成功后 invalidate 它，连带刷新所有打开中的详情查询。
 */
export const deploymentDetailsQueryKeys = {
  all: ['deployment-details'] as const,
  detail: (deploymentId: string | number | null) =>
    ['deployment-details', deploymentId] as const,
  forUpdate: (deploymentId: string | number | null) =>
    ['deployment-details-for-update', deploymentId] as const,
  forExtend: (deploymentId: string | number | null) =>
    ['deployment-details-for-extend', deploymentId] as const,
  containers: (deploymentId: string | number | null) =>
    ['deployment-containers', deploymentId] as const,
  detailsContainers: (deploymentId: string | number | null) =>
    ['deployment-details-containers', deploymentId] as const,
  logs: (
    deploymentId: string | number | null,
    containerId: string,
    stream: string
  ) => ['deployment-logs', deploymentId, containerId, stream] as const,
  hardwareTypes: ['deployment-hardware-types'] as const,
  availableReplicas: (hardwareId: string, gpuCount: number) =>
    ['deployment-available-replicas', hardwareId, gpuCount] as const,
  price: (
    hardwareId: string | undefined,
    gpuCount: number,
    durationHours: number,
    replicaCount: number,
    locationIds: unknown,
    currency: string | undefined
  ) =>
    [
      'deployment-price',
      hardwareId,
      gpuCount,
      durationHours,
      replicaCount,
      locationIds,
      currency,
    ] as const,
  extendPrice: (
    deploymentId: string | number | null,
    hours: number,
    priceParams: unknown
  ) => ['deployment-extend-price', deploymentId, hours, priceParams] as const,
  nameCheck: (resourceName: string) =>
    ['deployment-name-check', resourceName] as const,
  renameCheck: (trimmed: string) =>
    ['deployment-rename-check', trimmed] as const,
}
