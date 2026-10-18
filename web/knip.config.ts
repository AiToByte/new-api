import type { KnipConfig } from 'knip'

const config: KnipConfig = {
  // NOTE: src/components/ui used to be ignored here, which hid dead
  // vendored primitives (e.g. an entire chart component pulling in recharts
  // with zero consumers). Dead-code detection must see this directory; the
  // oxlint exclude for it stays (vendored shadcn style, see .oxlintrc.json).
  ignore: ['src/routeTree.gen.ts'],
  ignoreDependencies: ['tailwindcss', 'tw-animate-css'],
}

export default config
