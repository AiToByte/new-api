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
// 首屏体积预算门禁：解析 dist/index.html 直接引用的初始 JS/CSS，
// 统计 gzip 体积并与预算比较，超限即返回退出码 1，防止首屏优化成果
// 被"顺手引入一个依赖"这类回归悄悄吃掉。依赖先行的 `bun run build`
// 产物；只用 Node 内置模块，不引入额外依赖。
//
// 预算设定（2026-10-17 靶①-⑥ 完成后实测，约 +5% 余量）：
//   初始 JS 232.8KB gz -> 245KB；初始 CSS 61.5KB gz -> 65KB。
// 环境变量 BUDGET_INITIAL_JS_GZ / BUDGET_INITIAL_CSS_GZ 可临时覆盖预算，
// 仅用于本地演练门禁的失败路径，CI 使用内置预算。
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import zlib from 'node:zlib'

const dirName = path.dirname(fileURLToPath(import.meta.url))
const distDir = path.join(dirName, '..', 'dist')

const jsBudgetKb = Number(process.env.BUDGET_INITIAL_JS_GZ) || 245
const cssBudgetKb = Number(process.env.BUDGET_INITIAL_CSS_GZ) || 65

const htmlPath = path.join(distDir, 'index.html')
if (!fs.existsSync(htmlPath)) {
  console.error(
    'budget-check: dist/index.html 不存在，请先运行 `bun run build`'
  )
  process.exit(1)
}

const html = fs.readFileSync(htmlPath, 'utf8')
const refs = [
  ...html.matchAll(/(?:src|href)="(\/static\/(?:js|css)\/[^"]+)"/g),
].map((m) => m[1])
if (refs.length === 0) {
  console.error(
    'budget-check: dist/index.html 中未发现 /static 资源引用，构建产物异常'
  )
  process.exit(1)
}

const rows = []
const totals = { js: 0, css: 0 }
for (const ref of refs) {
  const buf = fs.readFileSync(path.join(distDir, ref))
  const gz = zlib.gzipSync(buf, { level: 6 }).length
  const kind = ref.endsWith('.js') ? 'js' : 'css'
  totals[kind] += gz
  rows.push({ ref, gz, kb: gz / 1024, kind })
}
rows.sort((a, b) => b.gz - a.gz)

for (const row of rows) {
  console.log(`${row.kb.toFixed(1).padStart(8)}KB gz  ${row.ref}`)
}

const breaches = []
if (totals.js / 1024 > jsBudgetKb) {
  breaches.push(
    `初始 JS gzip ${(totals.js / 1024).toFixed(1)}KB 超出预算 ${jsBudgetKb}KB`
  )
}
if (totals.css / 1024 > cssBudgetKb) {
  breaches.push(
    `初始 CSS gzip ${(totals.css / 1024).toFixed(1)}KB 超出预算 ${cssBudgetKb}KB`
  )
}

console.log(
  `initial JS ${(totals.js / 1024).toFixed(1)}KB / CSS ${(totals.css / 1024).toFixed(1)}KB gz  ` +
    `budget ${jsBudgetKb}KB / ${cssBudgetKb}KB`
)
if (breaches.length > 0) {
  for (const breach of breaches) {
    console.error(`budget-check: ${breach}`)
  }
  console.error(
    'budget-check: 请回退不必要的依赖引入或调整 chunk 划分，不要上调预算'
  )
  process.exit(1)
}
console.log('budget-check: PASS')
