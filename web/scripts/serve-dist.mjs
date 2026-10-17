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
// 生产产物冒烟用的轻量服务器：把 `bun run build` 生成的 dist/ 作为静态站点提供，
// 并把 /api 等接口前缀反代到真实后端，使浏览器可以直接启动真实构建产物。
// 仅用于本地与 CI 的产物验证，不参与发布；只用 Node 内置模块，不引入额外依赖。
import fs from 'node:fs'
import http from 'node:http'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const dirName = path.dirname(fileURLToPath(import.meta.url))
const distDir = path.resolve(dirName, '..', 'dist')
const port = Number(process.env.PROBE_PORT || 3200)
const backend = process.env.SERVER_URL || 'http://localhost:3000'
// 与前端 dev 代理保持一致：这些前缀的请求转发给后端，其余按静态资源处理。
const proxyPrefixes = ['/api', '/v1', '/mj', '/pg']

const contentTypes = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.map': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.gif': 'image/gif',
  '.webp': 'image/webp',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.ttf': 'font/ttf',
  '.txt': 'text/plain; charset=utf-8',
}

function sendFile(res, filePath, method) {
  const type =
    contentTypes[path.extname(filePath).toLowerCase()] ||
    'application/octet-stream'
  const size = fs.statSync(filePath).size
  res.writeHead(200, { 'Content-Type': type, 'Content-Length': size })
  if (method === 'HEAD') {
    res.end()
    return
  }
  fs.createReadStream(filePath).pipe(res)
}

function proxy(req, res, url) {
  const target = new URL(url.pathname + url.search, backend)
  const upstream = http.request(
    target,
    { method: req.method, headers: { ...req.headers, host: target.host } },
    (upstreamRes) => {
      res.writeHead(upstreamRes.statusCode || 502, upstreamRes.headers)
      upstreamRes.pipe(res)
    }
  )
  upstream.on('error', () => {
    if (!res.headersSent) {
      res.writeHead(502, { 'Content-Type': 'text/plain; charset=utf-8' })
    }
    res.end('backend unavailable')
  })
  req.pipe(upstream)
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, `http://localhost:${port}`)
  const proxied = proxyPrefixes.some(
    (prefix) => url.pathname === prefix || url.pathname.startsWith(`${prefix}/`)
  )
  if (proxied) {
    proxy(req, res, url)
    return
  }

  // 解析后的真实路径必须仍在 dist 目录内，避免目录穿越读取仓库其它文件。
  const candidate = path.join(
    distDir,
    path.normalize(decodeURIComponent(url.pathname))
  )
  if (
    candidate.startsWith(distDir) &&
    fs.existsSync(candidate) &&
    fs.statSync(candidate).isFile()
  ) {
    sendFile(res, candidate, req.method)
    return
  }
  // SPA 回退：未命中具体文件时返回 index.html，与后端 NoRoute 行为一致。
  sendFile(res, path.join(distDir, 'index.html'), req.method)
})

server.listen(port, () => {
  console.log(
    `dist smoke server: http://localhost:${port} -> ${distDir} (api -> ${backend})`
  )
})
