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
 * React Query cache keys for model pricing configuration and preview.
 *
 * `config` / `preview` 是前缀键：定价页按完整参数缓存，
 * 保存后 invalidate 前缀即整体刷新。
 */
export const modelPricingQueryKeys = {
  /** 定价配置（按配置名集合） */
  config: ['model-pricing-config'] as const,
  configFor: (names: string[]) => ['model-pricing-config', ...names] as const,
  /** 定价预览 */
  preview: ['model-pricing-preview'] as const,
  previewFor: (request: unknown) => ['model-pricing-preview', request] as const,
}
