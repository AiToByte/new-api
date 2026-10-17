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
import type { ISpec } from '@visactor/vchart'
import { registerAreaChart } from '@visactor/vchart/esm/chart/area/area'
import { registerBarChart } from '@visactor/vchart/esm/chart/bar/bar'
import { registerLineChart } from '@visactor/vchart/esm/chart/line/line'
import { registerPieChart } from '@visactor/vchart/esm/chart/pie/pie'
import { registerSankeyChart } from '@visactor/vchart/esm/chart/sankey/sankey'
import { registerCartesianBandAxis } from '@visactor/vchart/esm/component/axis/cartesian/band-axis'
import { registerCartesianLinearAxis } from '@visactor/vchart/esm/component/axis/cartesian/linear-axis'
import { registerLabel } from '@visactor/vchart/esm/component/label/label'
import { registerDiscreteLegend } from '@visactor/vchart/esm/component/legend/discrete/legend'
import { registerTitle } from '@visactor/vchart/esm/component/title/title'
import { registerTooltip } from '@visactor/vchart/esm/component/tooltip/tooltip'
import { VChart } from '@visactor/vchart/esm/core/vchart'
import { registerAreaSeriesAnimation } from '@visactor/vchart/esm/series/area/animation'
import { registerAreaSeries } from '@visactor/vchart/esm/series/area/area'
import { registerBarAnimation } from '@visactor/vchart/esm/series/bar/animation'
import { registerBarSeries } from '@visactor/vchart/esm/series/bar/bar'
import { registerLineSeries } from '@visactor/vchart/esm/series/line/line'
import {
  registerEmptyCircleAnimation,
  registerPieAnimation,
} from '@visactor/vchart/esm/series/pie/animation/animation'
import { registerPieSeries } from '@visactor/vchart/esm/series/pie/pie'
import { registerSankeySeries } from '@visactor/vchart/esm/series/sankey/sankey'

export const VCHART_OPTION = {
  // 与老前端保持一致（浏览器环境渲染优化）
  mode: 'desktop-browser',
} as const

// On-demand chart set: the app renders bar/line/area/pie/sankey with
// tooltip, cartesian axes, discrete legend, title and labels. Importing
// from these deep paths (instead of the package root) keeps map/geo,
// 3D and the other 20+ chart types out of the bundle (~0.7MB saved).
// Add the matching register call here when a new chart type or component
// is used anywhere, otherwise it renders nothing at runtime.
VChart.useRegisters([
  registerBarChart,
  registerBarSeries,
  registerBarAnimation,
  registerLineChart,
  registerLineSeries,
  registerAreaChart,
  registerAreaSeries,
  registerAreaSeriesAnimation,
  registerPieChart,
  registerPieSeries,
  registerPieAnimation,
  registerEmptyCircleAnimation,
  registerSankeyChart,
  registerSankeySeries,
  registerTooltip,
  registerCartesianBandAxis,
  registerCartesianLinearAxis,
  registerDiscreteLegend,
  registerTitle,
  registerLabel,
])

export const RegisteredVChart = VChart

// Apply the app-wide chart chrome (theme + transparent canvas) at the
// boundary where the dashboard's deliberately loose Record<string, any>
// spec builders meet vchart's strict ISpec union. Centralized so the cast
// lives in exactly one place instead of at every call site.
export function withChartChrome(
  spec: Record<string, any>,
  theme: string
): ISpec {
  return { ...spec, theme, background: 'transparent' } as ISpec
}
