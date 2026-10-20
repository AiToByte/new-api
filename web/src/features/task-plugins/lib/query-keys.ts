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
 * React Query cache keys for task plugins, plugin marketplace and plugin
 * version history.
 *
 * 约定：`detail(key)` 命中单个插件详情；`all` 是插件列表前缀，
 * invalidate `all` 会连带刷新依赖列表状态的页面，但不触碰详情缓存。
 */
export const taskPluginsQueryKeys = {
  /** 已安装插件列表 */
  all: ['task-plugins'] as const,
  /** 插件启用开关 */
  enabled: ['task-plugin-enabled'] as const,
  /** 插件声明选项（schema），供渠道类型徽标等消费 */
  options: ['task-plugin-options'] as const,
  /** 单个插件详情 */
  detail: (key: string) => ['task-plugin', key] as const,
  /** 指定版本的插件详情（版本对比） */
  detailWithVersion: (key: string, compareVersion: string) =>
    ['task-plugin', key, compareVersion] as const,
  /** 插件图标 */
  detailIcon: (key: string) => ['task-plugin', key, 'icon'] as const,
  /** 插件版本历史列表 */
  versions: ['task-plugin-versions'] as const,
  /** 单个插件的版本历史 */
  versionsOf: (key: string) => ['task-plugin-versions', key] as const,
  /** 插件市场索引（按来源） */
  marketplace: ['task-plugin-marketplace'] as const,
  marketplaceFor: (indexUrl?: string) =>
    ['task-plugin-marketplace', indexUrl] as const,
  /** 市场来源配置列表 */
  marketplaceSources: ['task-plugin-marketplace-sources'] as const,
  /** 市场来源内单个插件的安装明细 */
  marketplaceSource: (
    indexUrl: string,
    pluginKey: string,
    selectedVersion?: string,
    path?: string,
    sha256?: string
  ) =>
    [
      'task-plugin-marketplace-source',
      indexUrl,
      pluginKey,
      selectedVersion,
      path,
      sha256,
    ] as const,
  /** 市场插件图标（远端 URL + 可选 sha256 校验） */
  marketplaceIcon: (iconUrl: string, sha256?: string) =>
    ['task-plugin-marketplace-icon', iconUrl, sha256 ?? ''] as const,
  /** 市场插件更新日志 */
  marketplaceChangelog: (
    indexUrl: string,
    pluginKey: string,
    version: string,
    path: string | undefined,
    sha256: string | undefined,
    locale: string
  ) =>
    [
      'task-plugin-marketplace-changelog',
      indexUrl,
      pluginKey,
      version,
      path,
      sha256,
      locale,
    ] as const,
}
