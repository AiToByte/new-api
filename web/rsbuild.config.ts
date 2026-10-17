import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { defineConfig, loadEnv } from '@rsbuild/core'
import { pluginReact } from '@rsbuild/plugin-react'
import { pluginTailwindcss } from '@rsbuild/plugin-tailwindcss'
import { tanstackRouter } from '@tanstack/router-plugin/rspack'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

export default defineConfig(({ envMode }) => {
  const env = loadEnv({ mode: envMode, prefixes: ['VITE_'] })
  const serverUrl =
    process.env.VITE_REACT_APP_SERVER_URL ||
    env.rawPublicVars.VITE_REACT_APP_SERVER_URL ||
    'http://localhost:3000'

  const isProd = envMode === 'production'
  const devProxy = Object.fromEntries(
    (['/api', '/v1', '/mj', '/pg'] as const).map((key) => [
      key,
      { target: serverUrl, changeOrigin: true },
    ])
  ) as Record<string, { target: string; changeOrigin: boolean }>

  return {
    plugins: [pluginReact(), pluginTailwindcss({ optimize: false })],
    // Rsbuild 2: replaces deprecated `performance.chunkSplit` (RSPack 2 aligned)
    splitChunks: {
      preset: 'default',
      cacheGroups: {
        'vendor-react': {
          test: /node_modules[\\/](react|react-dom)[\\/]/,
          name: 'vendor-react',
          chunks: 'all',
          priority: 0,
          enforce: true,
        },
        'vendor-ui-primitives': {
          test: /node_modules[\\/](@base-ui|@radix-ui)[\\/]/,
          name: 'vendor-ui-primitives',
          chunks: 'all',
          priority: 0,
          enforce: true,
        },
        'vendor-tanstack': {
          // 路由与数据层：首屏同步图即需要（React 外壳、Query、Router），
          // 保持为初始 vendor 块以获得稳定的长期缓存哈希。
          test: /node_modules[\\/]@tanstack[\\/](react-router|router-core|react-query|query-core|react-store|store|history)[\\/]/,
          name: 'vendor-tanstack',
          chunks: 'all',
          priority: 0,
          enforce: true,
        },
        'vendor-tanstack-table': {
          // 表格与表格内核只在少数异步路由（数据列表页）中用到。
          // 限定 chunks: 'async'，避免它们被并入初始 vendor 块而进入首屏：
          // 之前的单一 vendor-tanstack 组使用 chunks: 'all'，会把仅异步使用的
          // table-core 合并进被首屏同步引用的块中，导致约 53KB 提前下发。
          test: /node_modules[\\/]@tanstack[\\/](react-table|table-core)[\\/]/,
          name: 'vendor-tanstack-table',
          chunks: 'async',
          priority: 0,
          enforce: true,
        },
      },
    },
    source: {
      entry: {
        index: './src/main.tsx',
      },
    },
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    html: {
      template: './index.html',
      favicon: './public/favicon.ico',
    },
    server: {
      host: '0.0.0.0',
      strictPort: false,
      proxy: devProxy,
    },
    output: {
      // Production optimizations
      minify: isProd,
      target: 'web',
      distPath: {
        root: 'dist',
      },
      // Rely on Rsbuild default legalComments ("linked" → per-chunk *.LICENSE.txt) in all modes.
      // Do not set "none" in production: that strips minifier-preserved third-party notices and
      // extracted license files, which some distributions require for open-source compliance.
    },
    performance: {
      // Remove console in production
      removeConsole: isProd ? ['log'] : false,
      buildCache: false,
    },
    tools: {
      rspack: {
        plugins: [
          tanstackRouter({
            target: 'react',
            // Dev: avoid per-route async chunks (reduces white flash on navigation + faster HMR feedback).
            // Prod: keep route-based code splitting.
            autoCodeSplitting: isProd,
          }),
        ],
        // Production strips the identity entries of en.json (key === value),
        // which i18next reproduces verbatim without the resource. The dev
        // server keeps the full file so missingKey debug logging is unchanged.
        module: {
          rules: isProd
            ? [
                {
                  test: /src[\\/]i18n[\\/]locales[\\/]en\.json$/,
                  use: [
                    {
                      loader: path.resolve(
                        __dirname,
                        './scripts/en-slim-loader.cjs'
                      ),
                    },
                  ],
                },
              ]
            : [],
        },
      },
    },
  }
})
